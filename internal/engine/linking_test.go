// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"os"
	"path/filepath"
	"testing"

	"nimblegate/internal/frames"
)

// TestEnabled_WildcardMatching exercises the isFrameEnabled wildcard logic
// to confirm `category/*` matches every frame in that category but doesn't
// leak across categories.
func TestEnabled_WildcardMatching(t *testing.T) {
	cases := []struct {
		id      string
		enabled []string
		want    bool
	}{
		{"git-safety/a", []string{"git-safety/*"}, true},
		{"git-safety/a", []string{"security/*"}, false},
		{"git-safety/folder-branch-lock", []string{"git-safety/folder-branch-lock"}, true},
		{"git-safety/folder-branch-lock", nil, true},        // empty = all enabled
		{"git-safety/folder-branch-lock", []string{}, true}, // empty slice = all enabled
		{"git-safety/folder-branch-lock", []string{"security/*"}, false},
		{"git-safety/folder-branch-lock", []string{"git-safety/*", "security/no-x"}, true},
		// Edge: trailing slash without star - should NOT match category-wide.
		{"git-safety/a", []string{"git-safety/"}, false},
		// Edge: bare prefix - should NOT match.
		{"git-safety/a", []string{"git-safety"}, false},
	}
	for _, tc := range cases {
		got := isFrameEnabled(tc.id, tc.enabled)
		if got != tc.want {
			t.Errorf("isFrameEnabled(%q, %v) = %v, want %v", tc.id, tc.enabled, got, tc.want)
		}
	}
}

// TestApplyOverride_SeverityOnly confirms the project's [frames.<id>]
// severity override is applied, but other frontmatter fields are untouched.
func TestApplyOverride_SeverityOnly(t *testing.T) {
	tmp := t.TempDir()
	cfg := `
[project]
name = "test"

[frames.security.no-innerHTML-user-input]
severity = "WARN"
`
	if err := os.WriteFile(filepath.Join(tmp, "appframes.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	stdlibFrames := []frames.Frame{
		{
			Frontmatter: frames.Frontmatter{
				Name:     "no-innerHTML-user-input",
				Category: frames.CategorySecurity,
				Severity: frames.SeverityBlock,
				Triggers: []string{"cli", "pre-commit"},
			},
			SourcePath: "stdlib:security/no-innerHTML-user-input.md",
		},
	}
	e, err := New(Options{ProjectRoot: tmp, StdlibFrames: stdlibFrames, CheckFuncs: map[string]CheckFunc{}})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	got, ok := e.Registry.Get("security/no-innerHTML-user-input")
	if !ok {
		t.Fatal("frame not registered")
	}
	if got.Frame.Frontmatter.Severity != frames.SeverityWarn {
		t.Errorf("Severity = %q, want WARN (override should have applied)", got.Frame.Frontmatter.Severity)
	}
	if len(got.Frame.Frontmatter.Triggers) != 2 {
		t.Errorf("Triggers altered by override: %v", got.Frame.Frontmatter.Triggers)
	}
}

// TestEngineNew_StdlibAndProjectFrameSameID - project frame must replace
// stdlib frame with the same ID, and the new triggers list must be honored.
func TestEngineNew_StdlibAndProjectFrameSameID(t *testing.T) {
	tmp := t.TempDir()
	stdlibFrames := []frames.Frame{
		{
			Frontmatter: frames.Frontmatter{
				Name:     "shared",
				Category: frames.CategoryGitSafety,
				Severity: frames.SeverityBlock,
				Triggers: []string{"cli"},
			},
			SourcePath: "stdlib:git-safety/shared.md",
		},
	}
	projectFrames := []frames.Frame{
		{
			Frontmatter: frames.Frontmatter{
				Name:     "shared",
				Category: frames.CategoryGitSafety,
				Severity: frames.SeverityWarn,
				Triggers: []string{"cli", "pre-commit"},
			},
			SourcePath: filepath.Join(tmp, ".appframes", "git-safety", "shared.md"),
		},
	}
	e, err := New(Options{
		ProjectRoot:   tmp,
		StdlibFrames:  stdlibFrames,
		ProjectFrames: projectFrames,
		CheckFuncs:    map[string]CheckFunc{},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	// Only one entry total under the shared ID.
	all := e.Registry.All()
	count := 0
	for _, rf := range all {
		if rf.Frame.ID() == "git/shared" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("frame appears %d times in registry, want 1 (project should have replaced stdlib)", count)
	}

	// pre-commit trigger must see the override's wider trigger list.
	if got := e.Registry.MatchingTrigger("pre-commit"); len(got) != 1 {
		t.Errorf("pre-commit matches = %d, want 1 (project frame added new trigger)", len(got))
	}
}

// TestRegistry_DuplicateProjectFrames - two project frames with the same ID
// should fail on the second AddProjectOverride? Currently it silently
// replaces the first. Confirm and document.
func TestRegistry_DuplicateProjectFrames(t *testing.T) {
	r := NewRegistry()
	f1 := makeFrame(frames.CategoryGitSafety, "dup", []string{"cli"})
	f1.SourcePath = "/project/a/dup.md"
	if err := r.AddProjectOverride(f1, nil); err != nil {
		t.Fatal(err)
	}
	f2 := makeFrame(frames.CategoryGitSafety, "dup", []string{"pre-commit"})
	f2.SourcePath = "/project/b/dup.md"
	// AddProjectOverride deletes existing and re-Adds - so this currently
	// succeeds silently. Document so future hardening is intentional.
	err := r.AddProjectOverride(f2, nil)
	if err != nil {
		t.Errorf("second AddProjectOverride errored: %v (current impl silently overwrites)", err)
	}
	// And the surviving frame is the second one.
	got, ok := r.Get("git/dup")
	if !ok {
		t.Fatal("frame missing from registry")
	}
	if got.Frame.SourcePath != f2.SourcePath {
		t.Errorf("surviving frame source = %q, want %q (last write wins)", got.Frame.SourcePath, f2.SourcePath)
	}
}
