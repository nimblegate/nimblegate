// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// cfPagesHeadersBaselineSet is the set of security headers we look for
// in a CF Pages `_headers` file. Each entry: header name (case-
// insensitive match), human description.
var cfPagesHeadersBaselineSet = []struct {
	Name        string
	Description string
}{
	{"Content-Security-Policy", "CSP - primary defense against XSS / data exfiltration"},
	{"X-Frame-Options", "clickjacking protection (or use frame-ancestors in CSP)"},
	{"X-Content-Type-Options", "MIME-sniff protection (set to `nosniff`)"},
	{"Referrer-Policy", "controls Referer header on outgoing requests"},
	{"Strict-Transport-Security", "HSTS - pins https for the host"},
}

// cfPagesHeaderLineRegex matches `<Header-Name>:` at the start of a
// header line within a _headers section. Case-insensitive name match.
func cfPagesHeaderLineRegex(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?im)^\s+` + regexp.QuoteMeta(name) + `\s*:`)
}

const cfPagesHeadersDisableMarker = "appframes:disable security/cf-pages-headers-baseline"
const cfPagesHeadersMaxFileBytes = 1 << 20 // 1 MiB

// CFPagesHeadersBaseline checks the CF Pages `_headers` file (when
// present) for the standard security-header baseline: CSP, X-Frame-
// Options, X-Content-Type-Options, Referrer-Policy, HSTS.
//
// When no `_headers` file exists, PASS - not every project uses CF Pages
// or that convention. When one exists but lacks a baseline header, WARN
// for each missing.
//
// The check operates on the entire file as one block: a header declared
// for ANY route (including `/*`) satisfies the baseline for that header.
// The frame doesn't enforce route-specific coverage - that's a
// per-project policy.
//
// Tier 2 WARN.
func CFPagesHeadersBaseline(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/cf-pages-headers-baseline",
		Category: frames.CategorySecurity,
	}

	// Look for _headers at any path; CF Pages convention is project-root
	// (`/_headers`) or static-output-dir (e.g. `public/_headers`,
	// `static/_headers`). We accept any of these.
	var headersFiles []string
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
		if d.Name() == "_headers" {
			headersFiles = append(headersFiles, path)
		}
		return nil
	})

	if len(headersFiles) == 0 {
		// No _headers file - not in scope. PASS.
		res.Outcome = engine.OutcomePass
		return res
	}

	var hits []string
	var hitsStruct []engine.Hit

	for _, file := range headersFiles {
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > cfPagesHeadersMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, cfPagesHeadersDisableMarker) {
			continue
		}
		for _, h := range cfPagesHeadersBaselineSet {
			re := cfPagesHeaderLineRegex(h.Name)
			if re.MatchString(content) {
				continue
			}
			label := fmt.Sprintf("%s not declared anywhere - %s", h.Name, h.Description)
			hits = append(hits, fmt.Sprintf("%s:0 - %s", file, label))
			hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: 0, Label: label})
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeWarn
	res.Reason = "_headers missing baseline security headers: " + strings.Join(hits, "; ")
	res.Fix = "add the missing headers to your _headers file under `/*` (or per-route as appropriate). Minimal baseline:\n\n/*\n  Content-Security-Policy: default-src 'self'\n  X-Frame-Options: DENY\n  X-Content-Type-Options: nosniff\n  Referrer-Policy: strict-origin-when-cross-origin\n  Strict-Transport-Security: max-age=31536000; includeSubDomains\n\nTune CSP for your actual sources; the others are usually safe as-is. See https://developers.cloudflare.com/pages/configuration/headers/ for the full syntax."
	return res
}
