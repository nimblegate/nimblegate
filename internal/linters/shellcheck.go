// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package linters

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"

	"nimblegate/internal/config"
	"nimblegate/internal/engine"
)

// ShellCheck drives `shellcheck --format=json`. shellcheck doesn't recurse, so
// nimblegate discovers the shell scripts (honoring [scan] excludes) and passes
// them as arguments. Patterns, if set, are used as the file list instead.
type ShellCheck struct{}

func (ShellCheck) ID() string { return frameID("shellcheck") }

func (s ShellCheck) Run(projectRoot string, cfg config.LinterConfig, excludedDirs []string) engine.CheckResult {
	id := s.ID()
	if _, err := exec.LookPath("shellcheck"); err != nil {
		return skipResult(id, "shellcheck: skipped (not found on PATH)")
	}
	files := cfg.Patterns
	if len(files) == 0 {
		files = discoverShellScripts(linterRoot(projectRoot, cfg), excludedDirs)
	}
	if len(files) == 0 {
		return buildResult(id, "shellcheck", nil, resolveOutcome(cfg.Severity), cfg.Disable) // no shell scripts → PASS
	}
	// -x + SCRIPTDIR: follow `source ./lib.sh` into the sourced file,
	// resolving relative to each script's own directory. Without these,
	// every consumer of a shared lib emits SC1091 "not following" - noise
	// that buries real findings, one false positive per consumer script.
	args := append([]string{"--format=json", "-x", "--source-path=SCRIPTDIR"}, files...)
	cmd := exec.Command("shellcheck", args...)
	cmd.Dir = projectRoot
	out, _ := cmd.Output() // non-zero exit when issues found is expected
	hits, ok := parseShellcheckJSON(out, projectRoot)
	if !ok {
		return skipResult(id, "shellcheck: skipped (no parseable JSON output)")
	}
	return buildResult(id, "shellcheck", hits, resolveOutcome(cfg.Severity), cfg.Disable)
}

// discoverShellScripts walks projectRoot for *.sh files, pruning excluded dirs.
func discoverShellScripts(root string, excludedDirs []string) []string {
	excluded := map[string]bool{".git": true}
	for _, d := range excludedDirs {
		excluded[d] = true
	}
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && excluded[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".sh") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

type shellcheckFinding struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Level   string `json:"level"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// parseShellcheckJSON parses `shellcheck --format=json` output. ok is false
// when the output isn't valid shellcheck JSON.
func parseShellcheckJSON(out []byte, projectRoot string) (hits []engine.Hit, ok bool) {
	var findings []shellcheckFinding
	if err := json.Unmarshal(out, &findings); err != nil {
		return nil, false
	}
	for _, f := range findings {
		hits = append(hits, engine.Hit{
			File:  relTo(f.File, projectRoot),
			Line:  f.Line,
			Label: fmt.Sprintf("SC%d (%s): %s", f.Code, f.Level, f.Message),
		})
	}
	return hits, true
}
