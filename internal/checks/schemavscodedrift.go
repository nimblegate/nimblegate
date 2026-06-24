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

// sqlColumnRegex extracts column names from CREATE TABLE and ALTER ADD
// COLUMN statements in schema.sql / migrations. It's deliberately loose:
// the column name is the FIRST identifier on a line inside a CREATE TABLE
// body OR the identifier after `ADD COLUMN` in an ALTER. Quoted forms
// (`"foo"`, `[foo]`, "`foo`") are normalized to bare names.
var sqlCreateTableRegex = regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?["` + "`" + `\[]?(\w+)["` + "`" + `\]]?\s*\((.+?)\)\s*(?:;|$|WITHOUT)`)
var sqlAlterAddColumnRegex = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+["` + "`" + `\[]?\w+["` + "`" + `\]]?\s+ADD\s+(?:COLUMN\s+)?["` + "`" + `\[]?(\w+)["` + "`" + `\]]?`)

// columnLineRegex extracts an identifier from each line of a CREATE TABLE
// body. Skips constraints (PRIMARY KEY, FOREIGN KEY, UNIQUE, CHECK, CONSTRAINT).
var columnLineRegex = regexp.MustCompile(`^\s*["` + "`" + `\[]?([A-Za-z_][A-Za-z0-9_]*)["` + "`" + `\]]?\s+`)

// codeColumnArrayRegex matches a Javascript/TypeScript const declaration
// of the shape:
//
//	const POST_LIST_COLS = ["id", "title", "country", ...];
//	export const PAGE_LIST_COLS = ['id', 'title'];
//
// The convention: UPPER_SNAKE name ending in _COLS or _COLUMNS or _FIELDS.
// Captures the array body in group 1.
var codeColumnArrayRegex = regexp.MustCompile(`(?s)(?:const|let|var|export\s+const)\s+([A-Z][A-Z0-9_]*(?:_COLS|_COLUMNS|_FIELDS))\s*(?::[^=]*)?=\s*\[(.+?)\]`)

// codeStringLiteralRegex extracts string literals from an array body.
// Accepts single, double, and backtick quotes.
var codeStringLiteralRegex = regexp.MustCompile(`["'` + "`" + `]([A-Za-z_][A-Za-z0-9_]*)["'` + "`" + `]`)

const schemaDriftDisableMarker = "appframes:disable database/schema-vs-code-drift"
const schemaDriftDisableLineMarker = "appframes:disable-next-line database/schema-vs-code-drift"
const schemaDriftMaxFileBytes = 1 << 20 // 1 MiB

// sqlApplicableFile is the file filter for collecting schema definitions.
// `schema.sql` at any depth, plus anything under a `migrations/` segment.
func sqlApplicableFile(path string) bool {
	if strings.ToLower(filepath.Ext(path)) != ".sql" {
		return false
	}
	base := strings.ToLower(filepath.Base(path))
	if base == "schema.sql" {
		return true
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, p := range parts {
		if strings.EqualFold(p, "migrations") || strings.EqualFold(p, "migration") {
			return true
		}
	}
	return false
}

// codeApplicableFileForSchemaDrift is the file filter for collecting the
// code-side column lists. Limited to db/ subdirectories to avoid scanning
// every JS/TS file in the project for unrelated UPPER_SNAKE_COLS constants
// (false positives multiply otherwise).
func codeApplicableFileForSchemaDrift(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".js", ".ts", ".mjs", ".cjs":
		// continue
	default:
		return false
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, p := range parts {
		pl := strings.ToLower(p)
		if pl == "db" || pl == "database" || pl == "models" {
			return true
		}
	}
	return false
}

// SchemaVsCodeDrift cross-references column-name string literals declared
// in code-side const arrays against the union of columns in the project's
// SQL schema (schema.sql + migrations/*.sql). Any code-side column that
// doesn't exist in the schema BLOCKs. This catches the originating
// incident before the migration runs: code expects a `country` column
// that hasn't been created yet → API would 500 in prod.
//
// Centerpiece frame for the migration footgun class. Independent of
// which migration script was or wasn't run; the gate fires on the
// static mismatch alone.
func SchemaVsCodeDrift(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "database/schema-vs-code-drift",
		Category: frames.CategoryAppCorrectness,
	}

	// Pass 1: scan SQL files to build the union set of declared columns.
	declaredCols := map[string]bool{}
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
		if !sqlApplicableFile(path) {
			return nil
		}
		info, statErr := os.Stat(path)
		if statErr != nil || info.Size() > schemaDriftMaxFileBytes {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		for _, name := range extractSchemaColumns(string(data)) {
			declaredCols[name] = true
		}
		return nil
	})

	// If we have no schema info, can't decide. Pass instead of BLOCKing -
	// the migration check covers the "no schema" case from another angle.
	if len(declaredCols) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}

	// Pass 2: scan code files for column-list arrays. Compare each string
	// literal against declaredCols.
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
			if codeApplicableFileForSchemaDrift(path) {
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
		if !codeApplicableFileForSchemaDrift(file) {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > schemaDriftMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, schemaDriftDisableMarker) {
			continue
		}

		for _, m := range codeColumnArrayRegex.FindAllStringSubmatchIndex(content, -1) {
			constName := content[m[2]:m[3]]
			body := content[m[4]:m[5]]
			// Compute the line of the declaration for reporting.
			declLine := 1 + strings.Count(content[:m[2]], "\n")

			// Honor line-disable.
			if declLine > 1 {
				lines := strings.Split(content, "\n")
				if strings.Contains(lines[declLine-2], schemaDriftDisableLineMarker) {
					continue
				}
			}

			for _, lm := range codeStringLiteralRegex.FindAllStringSubmatch(body, -1) {
				col := lm[1]
				if declaredCols[col] {
					continue
				}
				label := fmt.Sprintf("%s references column %q which is not declared in any schema.sql / migrations/*.sql", constName, col)
				hits = append(hits, fmt.Sprintf("%s:%d - %s", file, declLine, label))
				hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: declLine, Label: label})
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
	res.Reason = "code expects columns that do not exist in any committed schema: " + strings.Join(hits, "; ")
	res.Fix = "either (a) add the missing column to schema.sql + a migration, (b) update the code's column list to match the schema, or (c) if the column name is intentionally absent from the schema (computed field, alias, etc.) suppress with `// appframes:disable-next-line database/schema-vs-code-drift` above the const declaration."
	return res
}

// extractSchemaColumns returns the union of all column names declared in
// the given SQL text. Walks CREATE TABLE bodies and ALTER ADD COLUMN
// statements. Skips constraints inside CREATE TABLE bodies.
func extractSchemaColumns(sql string) []string {
	var names []string
	seen := map[string]bool{}
	addName := func(n string) {
		if seen[n] {
			return
		}
		seen[n] = true
		names = append(names, n)
	}

	for _, m := range sqlCreateTableRegex.FindAllStringSubmatch(sql, -1) {
		body := m[2]
		for _, line := range splitTableBody(body) {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			upper := strings.ToUpper(trimmed)
			// Skip table-level constraints - they aren't columns.
			if strings.HasPrefix(upper, "PRIMARY KEY") ||
				strings.HasPrefix(upper, "FOREIGN KEY") ||
				strings.HasPrefix(upper, "UNIQUE") ||
				strings.HasPrefix(upper, "CHECK") ||
				strings.HasPrefix(upper, "CONSTRAINT") {
				continue
			}
			if cm := columnLineRegex.FindStringSubmatch(line); cm != nil {
				addName(cm[1])
			}
		}
	}
	for _, m := range sqlAlterAddColumnRegex.FindAllStringSubmatch(sql, -1) {
		addName(m[1])
	}
	return names
}

// splitTableBody splits a CREATE TABLE body into definition entries.
// Naive comma-splitter that doesn't understand nested parens; for the
// vast majority of column definitions, this is sufficient. Edge cases
// (nested CHECK clauses with commas) are reported as constraint-shaped
// and skipped by the constraint filter anyway.
func splitTableBody(body string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	for _, r := range body {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, cur.String())
				cur.Reset()
				continue
			}
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
