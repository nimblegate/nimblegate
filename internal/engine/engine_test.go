// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"os"
	"path/filepath"
	"testing"

	"nimblegate/internal/frames"
)

func TestNew_AppliesProjectConfigEnabledList(t *testing.T) {
	tmp := t.TempDir()
	cfg := `
[project]
name = "test"

[frames]
enabled = ["git/yes"]
`
	if err := os.WriteFile(filepath.Join(tmp, "appframes.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	stdlibFrames := []frames.Frame{
		makeFrame(frames.CategoryGitSafety, "yes", []string{"cli"}),
		makeFrame(frames.CategoryGitSafety, "no", []string{"cli"}),
	}
	checks := map[string]CheckFunc{
		"git/yes": func(ctx CheckContext) CheckResult { return CheckResult{Outcome: OutcomePass} },
		"git/no":  func(ctx CheckContext) CheckResult { return CheckResult{Outcome: OutcomePass} },
	}
	e, err := New(Options{ProjectRoot: tmp, StdlibFrames: stdlibFrames, CheckFuncs: checks})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer e.Close()
	if _, ok := e.Registry.Get("git/yes"); !ok {
		t.Error("git/yes should be registered")
	}
	if _, ok := e.Registry.Get("git/no"); ok {
		t.Error("git/no should be filtered out by enabled list")
	}
}
