// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package linters

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	"nimblegate/internal/config"
	"nimblegate/internal/engine"
)

// ESLint drives `eslint --format json`. Prefers a project-local
// node_modules/.bin/eslint, then PATH.
type ESLint struct{}

func (ESLint) ID() string { return frameID("eslint") }

// eslintBin returns the eslint binary to use, or "" if none is found.
func eslintBin(projectRoot string) string {
	local := filepath.Join(projectRoot, "node_modules", ".bin", "eslint")
	if _, err := os.Stat(local); err == nil {
		return local
	}
	if p, err := exec.LookPath("eslint"); err == nil {
		return p
	}
	return ""
}

func (e ESLint) Run(projectRoot string, cfg config.LinterConfig, _ []string) engine.CheckResult {
	id := e.ID()
	runDir := linterRoot(projectRoot, cfg)
	bin := eslintBin(runDir)
	if bin == "" {
		return skipResult(id, "eslint: skipped (not found in node_modules/.bin or on PATH; set [linters.eslint] dir to the frontend subdir)")
	}
	targets := cfg.Patterns
	if len(targets) == 0 {
		targets = []string{"."}
	}
	args := append([]string{"--format", "json"}, targets...)
	cmd := exec.Command(bin, args...)
	cmd.Dir = runDir
	// eslint writes JSON to stdout and exits non-zero when it finds problems;
	// the non-zero exit is expected. Errors (bad config) go to stderr and
	// leave stdout non-JSON → SKIP rather than a false "clean".
	out, _ := cmd.Output()
	hits, ok := parseESLintJSON(out, projectRoot)
	if !ok {
		return skipResult(id, "eslint: skipped (no parseable JSON - is eslint configured for this project?)")
	}
	return buildResult(id, "eslint", hits, resolveOutcome(cfg.Severity), cfg.Disable)
}

type eslintFileResult struct {
	FilePath string `json:"filePath"`
	Messages []struct {
		RuleID  string `json:"ruleId"`
		Message string `json:"message"`
		Line    int    `json:"line"`
		Column  int    `json:"column"`
	} `json:"messages"`
}

// parseESLintJSON parses `eslint --format json` output into hits. ok is false
// when the output isn't valid eslint JSON (tool error) - distinct from a clean
// run (valid JSON, zero messages).
func parseESLintJSON(out []byte, projectRoot string) (hits []engine.Hit, ok bool) {
	var files []eslintFileResult
	if err := json.Unmarshal(out, &files); err != nil {
		return nil, false
	}
	for _, f := range files {
		for _, m := range f.Messages {
			label := m.Message
			if m.RuleID != "" {
				label = m.RuleID + ": " + m.Message
			}
			hits = append(hits, engine.Hit{File: relTo(f.FilePath, projectRoot), Line: m.Line, Label: label})
		}
	}
	return hits, true
}
