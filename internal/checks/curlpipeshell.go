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

// curlPipeShape is one detection pattern + a human label.
type curlPipeShape struct {
	Label   string
	Pattern *regexp.Regexp
}

var curlPipeShapes = []curlPipeShape{
	// Direct pipe to shell: curl ... | [sudo] (sh|bash|zsh|ksh|dash|ash)
	{
		Label:   "curl|wget piped to shell",
		Pattern: regexp.MustCompile(`\b(curl|wget)\b[^|\n]*\|\s*(sudo\s+)?(sh|bash|zsh|ksh|dash|ash)\b`),
	},
	// Pipe to eval: curl ... | eval (rare but documented attack form)
	{
		Label:   "curl|wget piped to eval",
		Pattern: regexp.MustCompile(`\b(curl|wget)\b[^|\n]*\|\s*(sudo\s+)?eval\b`),
	},
	// Process substitution: bash <(curl ...) / sh <(wget ...)
	{
		Label:   "shell with process-substituted curl/wget",
		Pattern: regexp.MustCompile(`\b(sh|bash|zsh|ksh|dash|ash)\s+<\(\s*(curl|wget)\b`),
	},
}

// curlPipeShellApplicableFile returns true if this path matches the
// frame's applies-to set. Centralised so the regex isn't re-computed
// per call.
func curlPipeShellApplicableFile(path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, "Dockerfile") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".sh", ".bash", ".zsh", ".ksh", ".dash", ".fish":
		return true
	}
	return false
}

const curlPipeShellDisableMarker = "appframes:disable commands/curl-pipe-shell"
const curlPipeShellDisableLineMarker = "appframes:disable-next-line commands/curl-pipe-shell"
const curlPipeShellMaxFileBytes = 1 << 20 // 1 MiB

// CurlPipeShell scans shell scripts + Dockerfiles for pipe-to-shell
// patterns (curl|sh, wget|bash, bash <(curl ...), etc.). Markdown
// documentation is NOT scanned because READMEs legitimately describe
// install commands.
//
// Scope contract (file-scan scope):
//   - cli + empty ChangedFiles → project-wide walk over applicable files
//   - pre-commit + empty ChangedFiles → PASS (matches real hook)
//   - non-empty ChangedFiles → scan only those (still filtered by extension)
//   - noise-dir exclusion uniform via IsExcluded
func CurlPipeShell(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "commands/curl-pipe-shell",
		Category: frames.CategoryCommands,
	}
	excludes := ctx.ExcludedDirs
	if len(excludes) == 0 {
		excludes = DefaultExcludes()
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
			if curlPipeShellApplicableFile(path) {
				files = append(files, path)
			}
			return nil
		})
	}

	// Parallel string + struct slices: the string view feeds the legacy
	// Reason rendering; the struct view feeds the V0.5 whitelist + dedup
	// pipeline. Both must stay aligned.
	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, file := range files {
		if !curlPipeShellApplicableFile(file) {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		if info.Size() > curlPipeShellMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, curlPipeShellDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], curlPipeShellDisableLineMarker) {
				continue
			}
			for _, shape := range curlPipeShapes {
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
	res.Reason = "pipe-to-shell patterns detected: " + strings.Join(hits, "; ")
	res.Fix = "replace with a checksum-pinned download + verify + exec, or a package manager install; for vetted bootstrap scripts add `# appframes:disable-next-line commands/curl-pipe-shell` above the line"
	return res
}
