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

// credentialPattern captures one issuer's detection regex, human name,
// and severity bucket. BLOCK patterns are real leaks (rotate now);
// INFO patterns are publishable-by-design tokens we surface for
// inventory but don't gate on.
//
// All regexes are anchored on word boundaries so a partial prefix
// embedded in another token doesn't match.
type credentialPattern struct {
	Name     string
	Pattern  *regexp.Regexp
	Severity engine.CheckOutcome // OutcomeBlock or OutcomeInfo
}

// credentialPatterns is the V0.5 detection catalog. BLOCK-severity
// entries are real leaks; INFO entries are publishable-by-design tokens
// we surface for inventory (so a swapped pk_live_/pk_test_ shows up).
var credentialPatterns = []credentialPattern{
	// AWS access key ID - 20 chars total: AKIA + 16 uppercase alphanumerics.
	{Name: "AWS access key", Pattern: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), Severity: engine.OutcomeBlock},

	// GitHub tokens. https://github.blog/2021-04-05-behind-githubs-new-authentication-token-formats/
	{Name: "GitHub personal access token (classic)", Pattern: regexp.MustCompile(`\bghp_[A-Za-z0-9]{36}\b`), Severity: engine.OutcomeBlock},
	{Name: "GitHub OAuth token", Pattern: regexp.MustCompile(`\bgho_[A-Za-z0-9]{36}\b`), Severity: engine.OutcomeBlock},
	{Name: "GitHub user-to-server token", Pattern: regexp.MustCompile(`\bghu_[A-Za-z0-9]{36}\b`), Severity: engine.OutcomeBlock},
	{Name: "GitHub server-to-server token", Pattern: regexp.MustCompile(`\bghs_[A-Za-z0-9]{36}\b`), Severity: engine.OutcomeBlock},
	{Name: "GitHub refresh token", Pattern: regexp.MustCompile(`\bghr_[A-Za-z0-9]{76}\b`), Severity: engine.OutcomeBlock},
	{Name: "GitHub fine-grained PAT", Pattern: regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{82}\b`), Severity: engine.OutcomeBlock},

	// Stripe secret + restricted keys - real credentials, BLOCK.
	{Name: "Stripe secret key (live)", Pattern: regexp.MustCompile(`\bsk_live_[A-Za-z0-9]{24,}\b`), Severity: engine.OutcomeBlock},
	{Name: "Stripe secret key (test)", Pattern: regexp.MustCompile(`\bsk_test_[A-Za-z0-9]{24,}\b`), Severity: engine.OutcomeBlock},
	{Name: "Stripe restricted key (live)", Pattern: regexp.MustCompile(`\brk_live_[A-Za-z0-9]{24,}\b`), Severity: engine.OutcomeBlock},
	{Name: "Stripe restricted key (test)", Pattern: regexp.MustCompile(`\brk_test_[A-Za-z0-9]{24,}\b`), Severity: engine.OutcomeBlock},

	// Stripe publishable keys - INTENTIONALLY PUBLIC. Catalogued at INFO
	// severity so users can audit where they appear (a swapped
	// pk_live_/pk_test_ in test fixtures or dev configs is the realistic
	// concern). Does NOT fail the commit gate.
	{Name: "Stripe publishable key (live)", Pattern: regexp.MustCompile(`\bpk_live_[A-Za-z0-9]{24,}\b`), Severity: engine.OutcomeInfo},
	{Name: "Stripe publishable key (test)", Pattern: regexp.MustCompile(`\bpk_test_[A-Za-z0-9]{24,}\b`), Severity: engine.OutcomeInfo},

	// Slack legacy + new tokens.
	{Name: "Slack token", Pattern: regexp.MustCompile(`\bxox[baprsoe]-[0-9A-Za-z-]{10,}\b`), Severity: engine.OutcomeBlock},

	// Google API key - AIza + 35 url-safe alphanumerics.
	{Name: "Google API key", Pattern: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`), Severity: engine.OutcomeBlock},
}

const credentialsDisableMarker = "appframes:disable security/no-hardcoded-credentials"
const credentialsDisableLineMarker = "appframes:disable-next-line security/no-hardcoded-credentials"

// credentialsMaxFileBytes caps the per-file scan size. Files larger than
// this are assumed binary / generated and skipped.
const credentialsMaxFileBytes = 1 << 20 // 1 MiB

// NoHardcodedCredentials scans files for committed secrets matching the
// curated prefix catalog. Reports BLOCK with file:line:pattern-name -
// the matched bytes are NEVER echoed (the audit log + terminal would
// otherwise amplify the leak).
//
// Scope contract follows the standard file-scanning convention:
//   - cli + empty ChangedFiles → project-wide walk
//   - pre-commit + ChangedFiles → those files only
//   - pre-commit + empty → PASS (matches the real hook with nothing staged)
//   - noise-dir exclusion applies uniformly
func NoHardcodedCredentials(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/no-hardcoded-credentials",
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

	// Hits are tracked per severity so the final outcome can be BLOCK
	// when any leak is found AND still surface publishable-key inventory
	// in the reason. Combined cap of 10 keeps output readable on
	// pathological inputs.
	//
	// Parallel structured-Hit slices feed the V0.5 dedup pass; the two
	// views (string + struct) must stay aligned.
	var blockHits, infoHits []string
	var blockHitsStruct, infoHitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, file := range files {
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		if info.Size() > credentialsMaxFileBytes {
			continue // skip large/binary files
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, credentialsDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			// Per-line opt-out: the line above this one has the marker.
			if i > 0 && strings.Contains(lines[i-1], credentialsDisableLineMarker) {
				continue
			}
			for _, p := range credentialPatterns {
				if p.Pattern.MatchString(line) {
					// Reason intentionally does NOT include the matched bytes.
					hit := fmt.Sprintf("%s:%d - %s", file, i+1, p.Name)
					hitStruct := engine.Hit{File: file, Line: i + 1, Label: p.Name}
					if p.Severity == engine.OutcomeBlock {
						blockHits = append(blockHits, hit)
						blockHitsStruct = append(blockHitsStruct, hitStruct)
					} else {
						infoHits = append(infoHits, hit)
						infoHitsStruct = append(infoHitsStruct, hitStruct)
					}
					if len(blockHits)+len(infoHits) >= hitCap {
						break filesLoop
					}
					// One finding per line is plenty for remediation.
					break
				}
			}
		}
	}

	switch {
	case len(blockHits) > 0:
		// Real leak - fail the gate. Reason lists everything (BLOCK + INFO)
		// so the user sees the full picture in one banner; the audit log
		// rolls up under one BLOCK entry per invocation.
		all := append([]string{}, blockHits...)
		all = append(all, infoHits...)
		allStruct := append([]engine.Hit{}, blockHitsStruct...)
		allStruct = append(allStruct, infoHitsStruct...)
		res.Outcome = engine.OutcomeBlock
		res.Reason = "credentials detected (raw bytes redacted): " + strings.Join(all, "; ")
		res.Fix = "remove the credential and ROTATE IT NOW (assume compromised); store via env var / secret manager; use `// appframes:disable-next-line security/no-hardcoded-credentials` ONLY for known-fake test fixtures"
		res.Hits = allStruct
	case len(infoHits) > 0:
		// Publishable keys only - informational catalogue. No "rotate"
		// language: these keys are intentionally public.
		res.Outcome = engine.OutcomeInfo
		res.Reason = "publishable keys catalogued (intentionally public - verify test/live wasn't swapped): " + strings.Join(infoHits, "; ")
		res.Hits = infoHitsStruct
	default:
		res.Outcome = engine.OutcomePass
	}
	return res
}
