// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// cidrRegex matches IPv4 CIDR strings in plaintext config files.
// IPv6 is intentionally out of scope for V1 - host-bit semantics are the
// same but the parsing surface is larger and the false-positive risk on
// arbitrary text is higher.
var cidrRegex = regexp.MustCompile(`\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})/(\d{1,2})\b`)

// cidrHostBitsApplicableFile returns true when the file is one of the
// config formats where a CIDR string is meaningful. The list errs broad
// because the CIDR-host-bits footgun shows up in CF dashboard automation,
// terraform, ufw rules, kubernetes manifests, and ad-hoc shell scripts.
func cidrHostBitsApplicableFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if base == "ufw.rules" || strings.HasPrefix(base, "ufw") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml", ".tf", ".tfvars", ".json", ".toml", ".conf", ".cfg", ".ini":
		return true
	case ".sh", ".bash", ".zsh":
		return true
	}
	return false
}

const cidrHostBitsDisableMarker = "appframes:disable network/cidr-host-bits-zero"
const cidrHostBitsDisableLineMarker = "appframes:disable-next-line network/cidr-host-bits-zero"
const cidrHostBitsMaxFileBytes = 1 << 20 // 1 MiB

// CIDRHostBitsZero scans config files for IPv4 CIDR strings with host bits
// set (e.g. 142.132.208.101/24 should be 142.132.208.0/24). Cloudflare,
// AWS Security Groups, GCP firewall, UFW, and Kubernetes NetworkPolicy all
// reject these - silently, in some cases (CF API returns error code 9109
// without an actionable message). The fix is mechanical: zero the host
// bits.
//
// Scope contract (file-scan scope conventions):
//   - cli + empty ChangedFiles → project-wide walk over applicable files
//   - pre-commit + empty ChangedFiles → PASS (matches real hook)
//   - non-empty ChangedFiles → scan only those
//   - skip noise via ShouldSkipPath
func CIDRHostBitsZero(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "network/cidr-host-bits-zero",
		Category: frames.CategoryNetwork,
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
			if cidrHostBitsApplicableFile(path) {
				files = append(files, path)
			}
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, file := range files {
		if !cidrHostBitsApplicableFile(file) {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > cidrHostBitsMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, cidrHostBitsDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], cidrHostBitsDisableLineMarker) {
				continue
			}
			for _, m := range cidrRegex.FindAllStringSubmatch(line, -1) {
				cidr := m[0]
				if !cidrHasHostBitsSet(cidr) {
					continue
				}
				label := fmt.Sprintf("CIDR %q has host bits set (suggest %s)", cidr, cidrCanonicalForm(cidr))
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
	res.Outcome = engine.OutcomeBlock
	res.Reason = "CIDR strings with host bits set: " + strings.Join(hits, "; ")
	res.Fix = "zero the host bits - e.g. 142.132.208.101/24 → 142.132.208.0/24. Cloudflare rejects with code 9109; AWS / GCP / UFW behave similarly. For a single octet that IS the network, use /32."
	return res
}

// cidrHasHostBitsSet reports whether the CIDR string has any host bits set.
// Returns false for malformed input (so we don't BLOCK on garbage that
// happens to match the regex shape).
func cidrHasHostBitsSet(cidr string) bool {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return !ip.Equal(ipnet.IP)
}

// cidrCanonicalForm returns the network form ("a.b.c.d/N" with host bits
// zeroed) for the given CIDR. Returns the input on parse failure (rare -
// caller validates first).
func cidrCanonicalForm(cidr string) string {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return cidr
	}
	return ipnet.String()
}
