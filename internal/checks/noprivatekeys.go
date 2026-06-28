// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// pemHeader describes one PEM block header we look for. Severity is per
// header because public certificates ride INFO while private keys ride
// BLOCK.
type pemHeader struct {
	Marker   string // the literal header line (substring match)
	Label    string // human-readable name for the reason
	Severity engine.CheckOutcome
}

var pemHeaders = []pemHeader{
	{Marker: "-----BEGIN RSA PRIVATE KEY-----", Label: "PEM RSA private key", Severity: engine.OutcomeBlock},
	{Marker: "-----BEGIN PRIVATE KEY-----", Label: "PEM PKCS#8 private key", Severity: engine.OutcomeBlock},
	{Marker: "-----BEGIN ENCRYPTED PRIVATE KEY-----", Label: "PEM encrypted PKCS#8 private key", Severity: engine.OutcomeBlock},
	{Marker: "-----BEGIN OPENSSH PRIVATE KEY-----", Label: "OpenSSH private key", Severity: engine.OutcomeBlock},
	{Marker: "-----BEGIN DSA PRIVATE KEY-----", Label: "PEM DSA private key", Severity: engine.OutcomeBlock},
	{Marker: "-----BEGIN EC PRIVATE KEY-----", Label: "PEM EC private key", Severity: engine.OutcomeBlock},
	{Marker: "-----BEGIN PGP PRIVATE KEY BLOCK-----", Label: "PGP private key", Severity: engine.OutcomeBlock},

	// Public certs - catalogued at INFO. They're not secret, but inventory
	// is useful for migrations / CA chain audits.
	{Marker: "-----BEGIN CERTIFICATE-----", Label: "X.509 certificate", Severity: engine.OutcomeInfo},
}

// privateKeyBasenames are bare filenames (no path) that conventionally
// hold an SSH private key. The .pub variant is the public counterpart
// and explicitly excluded below.
var privateKeyBasenames = map[string]bool{
	"id_rsa":     true,
	"id_dsa":     true,
	"id_ed25519": true,
	"id_ecdsa":   true,
}

// privateKeyExtensions are file suffixes that almost always hold a
// private key or keystore. Severity per extension because .crt/.cer
// are public certs.
var privateKeyExtensions = map[string]engine.CheckOutcome{
	".pem": engine.OutcomeBlock,
	".key": engine.OutcomeBlock,
	".p12": engine.OutcomeBlock,
	".pfx": engine.OutcomeBlock,
	".jks": engine.OutcomeBlock,
	".crt": engine.OutcomeInfo,
	".cer": engine.OutcomeInfo,
}

const privateKeysDisableMarker = "appframes:disable security/no-private-keys-in-repo"
const privateKeysDisableLineMarker = "appframes:disable-next-line security/no-private-keys-in-repo"
const privateKeysMaxFileBytes = 1 << 20 // 1 MiB

// NoPrivateKeysInRepo scans staged/working-tree files for PEM-armored
// private keys + known key filenames. Severity is mixed (BLOCK for
// private keys, INFO for public certs); see internal/stdlib/frames/security/no-private-keys-in-repo.md.
//
// Scope contract follows the standard file-scan scope conventions:
//   - cli + empty ChangedFiles → project-wide walk
//   - pre-commit + ChangedFiles → those only
//   - pre-commit + empty → PASS
//   - noise-dir exclusion uniform
//
// Redaction guarantee: the matched content is NEVER echoed. Reason
// reports file:line:label only.
func NoPrivateKeysInRepo(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/no-private-keys-in-repo",
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

	// Hits are tracked twice: as strings for the existing Reason rendering
	// (backwards-compat) AND as structured engine.Hit values for the V0.5
	// dedup pass. The two views must stay aligned - when a finding goes
	// into blockHits/infoHits, the parallel structured entry goes into
	// blockHitsStruct/infoHitsStruct.
	var blockHits, infoHits []string
	var blockHitsStruct, infoHitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, file := range files {
		if ShouldSkipPath(ctx, file) {
			continue
		}

		// --- content detection runs FIRST (more specific than filename) ---
		// A `.pem` file with a real PEM header should report the header
		// label (e.g. "PEM RSA private key"), not the generic "key file
		// (.pem)". Both detections firing on the same file is redundant.
		contentMatched := false
		info, statErr := os.Stat(file)
		if statErr == nil && info.Size() <= privateKeysMaxFileBytes {
			if data, readErr := os.ReadFile(file); readErr == nil {
				content := string(data)
				if !strings.Contains(content, privateKeysDisableMarker) {
					lines := strings.Split(content, "\n")
					for i, line := range lines {
						if i > 0 && strings.Contains(lines[i-1], privateKeysDisableLineMarker) {
							continue
						}
						for _, h := range pemHeaders {
							if !strings.Contains(line, h.Marker) {
								continue
							}
							contentMatched = true
							hit := fmt.Sprintf("%s:%d - %s", file, i+1, h.Label)
							hitStruct := engine.Hit{File: file, Line: i + 1, Label: h.Label}
							if h.Severity == engine.OutcomeBlock {
								blockHits = append(blockHits, hit)
								blockHitsStruct = append(blockHitsStruct, hitStruct)
							} else {
								infoHits = append(infoHits, hit)
								infoHitsStruct = append(infoHitsStruct, hitStruct)
							}
							if len(blockHits)+len(infoHits) >= hitCap {
								break filesLoop
							}
							break // one PEM block per line
						}
					}
				} else {
					// File-level disable marker → don't fall through to filename
					// detection either; the project author chose to opt out.
					contentMatched = true
				}
			}
		}

		if contentMatched {
			continue
		}

		// --- filename detection runs only when content didn't match ---
		base := filepath.Base(file)
		if privateKeyBasenames[base] {
			label := fmt.Sprintf("SSH private key filename %q", base)
			hit := fmt.Sprintf("%s:0 - %s", file, label)
			blockHits = append(blockHits, hit)
			blockHitsStruct = append(blockHitsStruct, engine.Hit{File: file, Line: 0, Label: label})
			if len(blockHits)+len(infoHits) >= hitCap {
				break filesLoop
			}
			continue
		}
		ext := strings.ToLower(filepath.Ext(file))
		if sev, ok := privateKeyExtensions[ext]; ok {
			label := "key file (" + ext + ")"
			if sev == engine.OutcomeInfo {
				label = "certificate file (" + ext + ")"
			}
			hit := fmt.Sprintf("%s:0 - %s", file, label)
			hitStruct := engine.Hit{File: file, Line: 0, Label: label}
			if sev == engine.OutcomeBlock {
				blockHits = append(blockHits, hit)
				blockHitsStruct = append(blockHitsStruct, hitStruct)
			} else {
				infoHits = append(infoHits, hit)
				infoHitsStruct = append(infoHitsStruct, hitStruct)
			}
			if len(blockHits)+len(infoHits) >= hitCap {
				break filesLoop
			}
		}
	}

	switch {
	case len(blockHits) > 0:
		all := append([]string{}, blockHits...)
		all = append(all, infoHits...)
		allStruct := append([]engine.Hit{}, blockHitsStruct...)
		allStruct = append(allStruct, infoHitsStruct...)
		res.Outcome = engine.OutcomeBlock
		res.Reason = "private keys detected (content redacted): " + strings.Join(all, "; ")
		res.Fix = "remove the key, REGENERATE IT (assume compromised), store via secret manager / KMS; commit only the `.pub` counterpart when a public copy is needed; or add the file to `[scan].exclude` if it's a known test fixture you cannot move"
		res.Hits = allStruct
	case len(infoHits) > 0:
		res.Outcome = engine.OutcomeInfo
		res.Reason = "certificates catalogued (public - verify intent): " + strings.Join(infoHits, "; ")
		res.Hits = infoHitsStruct
	default:
		res.Outcome = engine.OutcomePass
	}
	return res
}
