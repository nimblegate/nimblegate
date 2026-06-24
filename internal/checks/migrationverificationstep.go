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

// migrationVerificationPatterns are the substrings whose presence in a
// wrapper script satisfies "you verified the change went through." The
// list is broad on purpose - false positives on patterns that look like
// verifications but aren't are cheaper than false negatives that pass an
// unverified migration.
var migrationVerificationPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bpragma_table_info\b`),      // SQLite / D1
	regexp.MustCompile(`(?i)\bEXPLAIN\s+QUERY\s+PLAN\b`), // SQLite / Postgres
	regexp.MustCompile(`(?i)\bdescribe\s+\w+`),           // MySQL / generic
	regexp.MustCompile(`(?i)\bshow\s+tables\b`),          // MySQL / D1
	regexp.MustCompile(`(?i)\bshow\s+columns\b`),         // MySQL
	regexp.MustCompile(`(?i)\binformation_schema\.\w+`),  // Postgres / MySQL
	regexp.MustCompile(`\\d\+`),                          // psql describe shortcut - literal backslash-d-plus
	regexp.MustCompile(`(?i)\bSELECT\s+name\s+FROM\s+sqlite_master\b`),
	// `wrangler d1 execute "$DB" --command "SELECT ..."` is the canonical
	// post-apply verification pattern for D1.
	regexp.MustCompile(`(?i)wrangler\s+d1\s+execute[^\n]*--command\s+["'][^"']*SELECT`),
}

// migrationVerificationApplyRegex matches the DDL-applying line in a
// wrapper. If we see this AND no verification pattern, BLOCK.
var migrationVerificationApplyRegex = regexp.MustCompile(`(?im)^[^#\n]*\b(?:wrangler\s+d1\s+execute[^\n]*--file=|wrangler\s+kv\b|wrangler\s+r2\b|gcloud\s+sql\s+\w+|psql\s+[^|]*-f\s|mysql\s+[^|]*<|mongosh\s+--file)`)

// migrationVerificationWrapperRegex matches the filename convention for
// wrapper scripts. This is the SAME convention used by the
// migration-script-explicit-env frame, ensuring the two frames cover the
// same population.
func migrationVerificationApplicableFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if !strings.HasPrefix(base, "apply-") {
		return false
	}
	if !strings.Contains(base, "migration") {
		return false
	}
	// Accept .sh/.bash/.zsh and extensionless.
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".sh", ".bash", ".zsh", "":
		return true
	}
	return false
}

const migrationVerificationDisableMarker = "appframes:disable database/migration-verification-step"
const migrationVerificationMaxFileBytes = 1 << 20 // 1 MiB

// MigrationVerificationStep gates apply-*-migration* wrapper scripts on
// the presence of a post-apply verification step. The wrapper applies
// DDL via a multi-env CLI (wrangler / gcloud / psql / etc.); if the same
// wrapper never queries the target afterwards to confirm the change is
// visible, a silent network/auth/quota failure on the apply call goes
// undetected - exactly the failure mode that produced the original
// wrong-DB incident.
//
// File-level disable is honored: a wrapper marked
// `# appframes:disable database/migration-verification-step`
// (with a justification comment) passes.
//
// Reference: cf-incidents §1 frame proposal #2.
func MigrationVerificationStep(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "database/migration-verification-step",
		Category: frames.CategoryDatabase,
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
			if migrationVerificationApplicableFile(path) {
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
		if !migrationVerificationApplicableFile(file) {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > migrationVerificationMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, migrationVerificationDisableMarker) {
			continue
		}
		applyLoc := migrationVerificationApplyRegex.FindStringIndex(content)
		if applyLoc == nil {
			// No actual DDL apply found - not in scope.
			continue
		}
		// At least one verification pattern must match anywhere in the file.
		verified := false
		for _, re := range migrationVerificationPatterns {
			if re.MatchString(content) {
				verified = true
				break
			}
		}
		if verified {
			continue
		}
		// Compute line of the apply for reporting.
		line := 1 + strings.Count(content[:applyLoc[0]], "\n")
		label := "wrapper applies DDL but has no post-apply verification (pragma_table_info / DESCRIBE / SHOW / wrangler d1 execute --command SELECT)"
		hits = append(hits, fmt.Sprintf("%s:%d - %s", file, line, label))
		hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: line, Label: label})
		if len(hits) >= hitCap {
			break filesLoop
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeBlock
	res.Reason = "migration wrappers without verification: " + strings.Join(hits, "; ")
	res.Fix = "after the apply call, query the target env with the same CLI to confirm the change is visible - e.g. for D1: `wrangler d1 execute \"$DB\" --command \"SELECT name FROM pragma_table_info('posts') WHERE name = 'country';\" --remote` and assert the result is non-empty. This catches silent network/auth/quota failures on the apply itself."
	return res
}
