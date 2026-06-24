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

// sqliteMigrationShapes describes the destructive / non-idempotent DDL
// patterns that need a wrapper. Each shape carries a human label used
// in the violation reason.
type sqliteMigrationShape struct {
	Label   string
	Pattern *regexp.Regexp
}

var sqliteMigrationShapes = []sqliteMigrationShape{
	// ALTER TABLE ... ADD COLUMN - re-running errors with "duplicate column".
	{
		Label:   "ALTER TABLE ... ADD COLUMN (re-run fails with duplicate column)",
		Pattern: regexp.MustCompile(`(?i)\bALTER\s+TABLE\s+[^\s;]+\s+ADD\s+(?:COLUMN\s+)?`),
	},
	// DROP COLUMN - re-running errors with "no such column".
	{
		Label:   "ALTER TABLE ... DROP COLUMN (re-run fails with no-such-column)",
		Pattern: regexp.MustCompile(`(?i)\bALTER\s+TABLE\s+[^\s;]+\s+DROP\s+(?:COLUMN\s+)?`),
	},
	// RENAME COLUMN - re-running errors with "no such column".
	{
		Label:   "ALTER TABLE ... RENAME COLUMN (re-run fails)",
		Pattern: regexp.MustCompile(`(?i)\bALTER\s+TABLE\s+[^\s;]+\s+RENAME\s+(?:COLUMN\s+)?`),
	},
	// RENAME TABLE - re-running errors with "no such table".
	{
		Label:   "ALTER TABLE ... RENAME TO (re-run fails)",
		Pattern: regexp.MustCompile(`(?i)\bALTER\s+TABLE\s+[^\s;]+\s+RENAME\s+TO\s+`),
	},
}

// sqliteIdempotentOptOut is the inline opt-out comment authors can leave
// in a one-time historical migration. Anything else triggers the gate.
const sqliteIdempotentOptOut = "IDEMPOTENT-WRAPPER-NOT-REQUIRED"

const sqliteMigrationDisableMarker = "appframes:disable database/sqlite-migration-idempotent-wrapper"
const sqliteMigrationDisableLineMarker = "appframes:disable-next-line database/sqlite-migration-idempotent-wrapper"
const sqliteMigrationMaxFileBytes = 1 << 20 // 1 MiB

// sqliteMigrationApplicableFile returns true when the file is a SQL
// migration. The check is path-based: `.sql` files under any directory
// segment named `migrations` (case-insensitive). Standalone .sql files
// outside a migrations/ tree are NOT scanned - they're likely seed data
// or ad-hoc queries.
func sqliteMigrationApplicableFile(path string) bool {
	if strings.ToLower(filepath.Ext(path)) != ".sql" {
		return false
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, p := range parts {
		if strings.EqualFold(p, "migrations") || strings.EqualFold(p, "migration") {
			return true
		}
	}
	return false
}

// SQLiteMigrationIdempotentWrapper scans .sql migration files for
// destructive / non-idempotent DDL (ALTER TABLE ADD/DROP/RENAME COLUMN,
// RENAME TO) and BLOCKs unless one of two conditions holds:
//
//  1. A wrapper script exists alongside the .sql under any scripts/ or
//     scripts/* directory with a name matching apply-*-migration*. The
//     wrapper is presumed to use pragma_table_info to gate the ALTER.
//  2. The .sql file contains an inline opt-out comment with the literal
//     string "IDEMPOTENT-WRAPPER-NOT-REQUIRED" (for one-time historical
//     migrations the user has audited).
//
// Reference incident: a multi-country migration on 2026-05-16. Raw
// .sql worked on first apply; re-running errored on duplicate column,
// stopping the batch and leaving later index-creates skipped. Wrapper
// script was the right fix (then it sprouted its own wrong-env
// footgun - covered by migration-script-explicit-env).
//
// Generalizes beyond SQLite - Postgres, MySQL, D1, MongoDB all have
// the same "raw DDL is not idempotent" problem, just with different
// error messages. The frame fires identically.
func SQLiteMigrationIdempotentWrapper(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "database/sqlite-migration-idempotent-wrapper",
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
			if sqliteMigrationApplicableFile(path) {
				files = append(files, path)
			}
			return nil
		})
	}

	// Pre-scan the project for wrapper scripts (apply-*-migration*) so we
	// can answer "is there a wrapper for this migration?" without re-walking
	// for every SQL file.
	wrapperPresent := projectHasMigrationWrappers(ctx)

	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, file := range files {
		if !sqliteMigrationApplicableFile(file) {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > sqliteMigrationMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, sqliteMigrationDisableMarker) {
			continue
		}
		// File-level opt-out (the explicit historical-migration escape hatch).
		if strings.Contains(content, sqliteIdempotentOptOut) {
			continue
		}
		// Whole-project wrapper presence is the OTHER escape hatch - if
		// the project ships apply-*-migration* scripts under scripts/, the
		// wrapper-gated path is the canonical workflow and we don't BLOCK
		// individual .sql files. The migration-script-explicit-env frame
		// then takes over and gates the wrapper itself.
		if wrapperPresent {
			continue
		}

		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], sqliteMigrationDisableLineMarker) {
				continue
			}
			// Skip SQL comments (-- prefix) so README-shaped migrations
			// don't trip the gate.
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "--") {
				continue
			}
			for _, shape := range sqliteMigrationShapes {
				if shape.Pattern.MatchString(line) {
					hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, shape.Label))
					hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: shape.Label})
					if len(hits) >= hitCap {
						break filesLoop
					}
					break // one finding per line
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
	res.Reason = "non-idempotent SQL DDL without a wrapper script: " + strings.Join(hits, "; ")
	res.Fix = "either (a) add a wrapper at scripts/apply-<feature>-migration.sh that uses pragma_table_info to gate the ALTER, or (b) for a one-time historical migration add a top-of-file comment `-- IDEMPOTENT-WRAPPER-NOT-REQUIRED` documenting why it's safe to leave unguarded."
	return res
}

// projectHasMigrationWrappers walks the project's scripts/ directories
// looking for shell scripts matching apply-*-migration*. Cached per check
// run via the ctx (no - keep it simple, just walk once).
func projectHasMigrationWrappers(ctx engine.CheckContext) bool {
	scriptsDir := filepath.Join(ctx.ProjectRoot, "scripts")
	if _, err := os.Stat(scriptsDir); err != nil {
		return false
	}
	found := false
	_ = filepath.WalkDir(scriptsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		base := strings.ToLower(d.Name())
		if strings.HasPrefix(base, "apply-") && strings.Contains(base, "migration") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
