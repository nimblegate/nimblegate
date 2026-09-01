// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const dbdumpDisableMarker = "appframes:disable security/no-database-dump-in-repo"

// dbdumpMagicPrefixes are binary dump signatures checked against the raw
// leading bytes BEFORE the usual NUL-byte binary skip - these formats are
// binary by design and would otherwise never be seen.
var dbdumpMagicPrefixes = []struct {
	Prefix []byte
	Label  string
}{
	{[]byte("PGDMP"), "pg_dump custom-format archive"},
	{[]byte("SQLite format 3\x00"), "SQLite database file"},
}

// dbdumpTextMarkers are tool-written headers that machine-generated dumps
// always carry near the top. Hand-written seed SQL does not contain them,
// which is exactly the line this frame draws.
var dbdumpTextMarkers = []struct {
	Marker string
	Label  string
}{
	{"-- MySQL dump ", "mysqldump output"},
	{"-- PostgreSQL database dump", "pg_dump output"},
	{"-- MariaDB dump ", "mariadb-dump output"},
}

// dbdumpHeaderWindow bounds how far into the file the text markers are
// looked for - dump tools write their banner in the first lines.
const dbdumpHeaderWindow = 2048

// NoDatabaseDumpInRepo detects machine-generated database dumps committed
// to the repository. A dump is a snapshot of real data - customer records,
// credentials, tokens - and does not belong in git history. Detection keys
// on tool-written signatures (mysqldump/pg_dump banners, PGDMP and SQLite
// magic), so hand-written seed/fixture SQL never fires.
//
// Scope contract follows the standard file-scan scope conventions:
//   - cli + empty ChangedFiles → project-wide walk
//   - pre-commit + ChangedFiles → those only
//   - pre-commit + empty → PASS
//   - noise-dir exclusion uniform
//
// Redaction guarantee: dump content is NEVER echoed.
func NoDatabaseDumpInRepo(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/no-database-dump-in-repo",
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

		// --- binary magic runs on raw bytes, before any NUL skip ---
		for _, m := range dbdumpMagicPrefixes {
			if bytes.HasPrefix(data, m.Prefix) {
				hits = append(hits, fmt.Sprintf("%s:0 - %s", file, m.Label))
				hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: 0, Label: m.Label})
				if len(hits) >= hitCap {
					break filesLoop
				}
				continue filesLoop
			}
		}

		if bytes.IndexByte(data, 0) >= 0 {
			continue
		}
		content := string(data)
		if fileDisabledByMarker(content, dbdumpDisableMarker) {
			continue
		}
		window := content
		if len(window) > dbdumpHeaderWindow {
			window = window[:dbdumpHeaderWindow]
		}
		for _, m := range dbdumpTextMarkers {
			idx := strings.Index(window, m.Marker)
			if idx < 0 {
				continue
			}
			line := 1 + strings.Count(window[:idx], "\n")
			hits = append(hits, fmt.Sprintf("%s:%d - %s", file, line, m.Label))
			hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: line, Label: m.Label})
			if len(hits) >= hitCap {
				break filesLoop
			}
			break // one marker per file is enough
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeWarn
	res.Reason = "database dump committed (content redacted): " + strings.Join(hits, "; ")
	res.Fix = "remove the dump from the push; if it contains real records treat it as a data incident (scrub history, assess exposure); keep hand-written seed/fixture SQL instead - it does not trip this frame; for intentional dump fixtures add `appframes:disable security/no-database-dump-in-repo` inside the file or whitelist it with a written reason"
	return res
}
