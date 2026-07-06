// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

var (
	piiCardRe = regexp.MustCompile(`(?:^|[^0-9])((?:\d[ -]?){12,18}\d)(?:[^0-9]|$)`)
	piiIbanRe = regexp.MustCompile(`\b([A-Z]{2}\d{2}[A-Z0-9]{10,30})\b`)
	piiSSNRe  = regexp.MustCompile(`\b(\d{3})-(\d{2})-(\d{4})\b`)
)

const piiDisableMarker = "appframes:disable security/no-pii-in-source"
const piiDisableLineMarker = "appframes:disable-next-line security/no-pii-in-source"

// piiKnownTestCards are publishable-by-design test numbers (Stripe docs,
// scheme documentation). Fake by design: excluded entirely, not downgraded.
var piiKnownTestCards = map[string]bool{
	"4242424242424242": true,
	"4111111111111111": true,
	"4012888888881881": true,
	"4000056655665556": true,
	"4222222222222":    true,
	"5555555555554444": true,
	"5200828282828210": true,
	"5105105105105100": true,
	"2223003122003222": true,
	"378282246310005":  true,
	"371449635398431":  true,
	"6011111111111117": true,
	"6011000990139424": true,
	"3566002020360505": true,
}

// piiKnownExampleIBANs appear across banking documentation and tutorials.
var piiKnownExampleIBANs = map[string]bool{
	"GB82WEST12345698765432": true,
	"GB33BUKB20201555555555": true,
	"GB94BARC10201530093459": true,
	"DE89370400440532013000": true,
}

// piiIbanLengths is the national IBAN length per country code (common
// European/EEA set). Countries not listed are not validated - shape without
// a length rule is too weak a signal.
var piiIbanLengths = map[string]int{
	"AT": 20, "BE": 16, "BG": 22, "CH": 21, "CY": 28, "CZ": 24, "DE": 22,
	"DK": 18, "EE": 20, "ES": 24, "FI": 18, "FR": 27, "GB": 22, "GR": 27,
	"HR": 21, "HU": 28, "IE": 22, "IS": 26, "IT": 27, "LI": 21, "LT": 20,
	"LU": 20, "LV": 21, "MT": 31, "NL": 18, "NO": 15, "PL": 28, "PT": 25,
	"RO": 24, "SE": 24, "SI": 19, "SK": 24,
}

func piiLuhnValid(digits string) bool {
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// piiCardScheme reports whether the digit string carries a known scheme
// prefix at a length that scheme issues.
func piiCardScheme(d string) bool {
	n := len(d)
	switch {
	case d[0] == '4':
		return n == 13 || n == 16 || n == 19
	case d[0] == '5' && d[1] >= '1' && d[1] <= '5':
		return n == 16
	case strings.HasPrefix(d, "222") || strings.HasPrefix(d, "223") ||
		strings.HasPrefix(d, "224") || strings.HasPrefix(d, "225") ||
		strings.HasPrefix(d, "226") || strings.HasPrefix(d, "227"):
		return n == 16
	case strings.HasPrefix(d, "34") || strings.HasPrefix(d, "37"):
		return n == 15
	case strings.HasPrefix(d, "6011") || strings.HasPrefix(d, "65") ||
		(strings.HasPrefix(d, "64") && d[2] >= '4'):
		return n == 16
	case strings.HasPrefix(d, "35"):
		return n == 16
	}
	return false
}

func piiIbanValid(iban string) bool {
	want, ok := piiIbanLengths[iban[:2]]
	if !ok || len(iban) != want {
		return false
	}
	rearranged := iban[4:] + iban[:4]
	rem := 0
	for _, c := range rearranged {
		switch {
		case c >= '0' && c <= '9':
			rem = (rem*10 + int(c-'0')) % 97
		case c >= 'A' && c <= 'Z':
			v := int(c-'A') + 10
			rem = (rem*100 + v) % 97
		default:
			return false
		}
	}
	return rem == 1
}

var piiSSNContext = []string{"ssn", "social security", "social_security", "socialsecurity", "tax id", "tax_id", "taxid", "taxpayer"}

func piiSSNContextOn(line string) bool {
	l := strings.ToLower(line)
	for _, kw := range piiSSNContext {
		if strings.Contains(l, kw) {
			return true
		}
	}
	return false
}

func piiSSNShapeValid(area, group, serial string) bool {
	if area == "000" || area == "666" || area[0] == '9' {
		return false
	}
	return group != "00" && serial != "0000"
}

// piiScanLine returns the labels of validated personal-data findings on one
// line. Matched values are never returned - only kind labels.
func piiScanLine(line string) []string {
	var labels []string
	for _, m := range piiCardRe.FindAllStringSubmatch(line, -1) {
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, m[1])
		if len(digits) < 13 || len(digits) > 19 {
			continue
		}
		if !piiCardScheme(digits) || !piiLuhnValid(digits) || piiKnownTestCards[digits] {
			continue
		}
		labels = append(labels, "payment card number (Luhn-valid)")
		break
	}
	for _, m := range piiIbanRe.FindAllStringSubmatch(line, -1) {
		if piiKnownExampleIBANs[m[1]] || !piiIbanValid(m[1]) {
			continue
		}
		labels = append(labels, "IBAN (checksum-valid)")
		break
	}
	if piiSSNContextOn(line) {
		for _, m := range piiSSNRe.FindAllStringSubmatch(line, -1) {
			if !piiSSNShapeValid(m[1], m[2], m[3]) {
				continue
			}
			labels = append(labels, "SSN with identifying context")
			break
		}
	}
	return labels
}

// NoPIIInSource detects real-looking personal data - payment card numbers,
// IBANs, US SSNs - in committed files. Every detector is checksum- or
// shape-validated; well-known publishable test values are excluded. The
// reason never echoes matched values.
//
// Scope contract (file-scan scope):
//   - cli + empty ChangedFiles → project-wide walk
//   - pre-commit + empty ChangedFiles → PASS (matches real hook)
//   - non-empty ChangedFiles → scan only those
//   - noise-dir exclusion uniform via ShouldSkipPath; binary files (NUL
//     byte) and files over 1 MiB skipped
func NoPIIInSource(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/no-pii-in-source",
		Category: frames.CategorySecurity,
	}

	files := ctx.ChangedFiles
	if len(files) == 0 && ctx.Trigger == engine.TriggerCLI {
		_ = filepath.WalkDir(ctx.ProjectRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if ShouldSkipPath(ctx, path) {
					return filepath.SkipDir
				}
				return nil
			}
			files = append(files, path)
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, file := range files {
		if ShouldSkipPath(ctx, file) {
			continue
		}
		data, ok := ReadFileBounded(file, DefaultMaxFileBytes)
		if !ok {
			continue
		}
		if bytes.IndexByte(data, 0) >= 0 {
			continue
		}
		content := string(data)
		if strings.Contains(content, piiDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], piiDisableLineMarker) {
				continue
			}
			for _, label := range piiScanLine(line) {
				hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, label))
				hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: label})
				if len(hits) >= hitCap {
					break filesLoop
				}
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeWarn
	res.Reason = "possible personal data detected (raw values redacted): " + strings.Join(hits, "; ")
	res.Fix = "replace real records with clearly-fake fixtures (Stripe test cards, documentation IBANs); if real customer data was committed, scrub history and treat as an incident; add `appframes:disable-next-line security/no-pii-in-source` above verified-fake values"
	return res
}
