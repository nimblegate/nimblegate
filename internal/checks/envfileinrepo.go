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

// envAssignRe matches an uncommented KEY=value line with a non-empty value.
// A committed .env that carries only comments or empty assignments is bad
// practice but leaks nothing; requiring one live assignment keeps the frame
// deterministic without flagging placeholder shells.
var envAssignRe = regexp.MustCompile(`^\s*(?:export\s+)?[A-Za-z_][A-Za-z0-9_]*\s*=\s*\S`)

// envAllowedSuffixes are the conventional committed-on-purpose variants:
// the final dot-segment of the basename decides (.env.example,
// .env.local.sample, ...). `.env.vault` is dotenv-vault's encrypted file,
// designed to be committed.
var envAllowedSuffixes = map[string]bool{
	"example":  true,
	"sample":   true,
	"template": true,
	"dist":     true,
	"vault":    true,
}

const envFileDisableMarker = "appframes:disable security/no-env-file-in-repo"

// envFileBasenameMatch reports whether the basename is a live environment
// file: exactly `.env`, or `.env.<variant>` where the final segment is not
// one of the committed-on-purpose suffixes.
func envFileBasenameMatch(base string) bool {
	if base == ".env" {
		return true
	}
	if !strings.HasPrefix(base, ".env.") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(base, ".env."), ".")
	return !envAllowedSuffixes[parts[len(parts)-1]]
}

// NoEnvFileInRepo detects live environment files (`.env`, `.env.production`,
// ...) committed to the repository. The values in an environment file are
// credentials by definition; once pushed they must be treated as leaked.
// Placeholder variants (`.env.example`, `.env.sample`, `.env.template`,
// `.env.dist`) and dotenv-vault's encrypted `.env.vault` are allowed.
//
// Scope contract follows the standard file-scan scope conventions:
//   - cli + empty ChangedFiles → project-wide walk
//   - pre-commit + ChangedFiles → those only
//   - pre-commit + empty → PASS
//   - noise-dir exclusion uniform
//
// Redaction guarantee: values are NEVER echoed; the reason reports the file
// and the line of the first live assignment only.
func NoEnvFileInRepo(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/no-env-file-in-repo",
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

	for _, file := range files {
		if ShouldSkipPath(ctx, file) {
			continue
		}
		if !envFileBasenameMatch(filepath.Base(file)) {
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
		if strings.Contains(content, envFileDisableMarker) {
			continue
		}
		for i, line := range strings.Split(content, "\n") {
			if envAssignRe.MatchString(line) {
				label := "environment file with live assignments"
				hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, label))
				hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: label})
				break
			}
		}
		if len(hits) >= hitCap {
			break
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeBlock
	res.Reason = "environment file committed (values redacted): " + strings.Join(hits, "; ")
	res.Fix = "remove the file from the push and ROTATE every value in it (assume leaked); keep a committed `.env.example` with placeholder values instead; load real values from the deployment environment or a secret manager"
	return res
}
