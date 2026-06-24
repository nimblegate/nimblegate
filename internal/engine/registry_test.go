// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"testing"

	"nimblegate/internal/frames"
)

func makeFrame(category frames.Category, name string, triggers []string) frames.Frame {
	return frames.Frame{
		Frontmatter: frames.Frontmatter{
			Name:     name,
			Category: category,
			Severity: frames.SeverityBlock,
			Triggers: triggers,
		},
		Body:       "test",
		SourcePath: "test://" + name,
	}
}

func TestRegistry_AddAndLookupByTrigger(t *testing.T) {
	r := NewRegistry()
	if err := r.Add(makeFrame(frames.CategoryGitSafety, "folder-lock", []string{"git-wrap", "pre-commit"}), nil); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(makeFrame(frames.CategorySecurity, "no-innerHTML", []string{"pre-commit"}), nil); err != nil {
		t.Fatal(err)
	}

	matched := r.MatchingTrigger("git-wrap")
	if len(matched) != 1 || matched[0].Frame.Frontmatter.Name != "folder-lock" {
		t.Errorf("git-wrap matched = %v, want [folder-lock]", names(matched))
	}
	matched = r.MatchingTrigger("pre-commit")
	if len(matched) != 2 {
		t.Errorf("pre-commit matched count = %d, want 2", len(matched))
	}
}

func TestRegistry_AddDuplicateIDFails(t *testing.T) {
	r := NewRegistry()
	if err := r.Add(makeFrame(frames.CategoryGitSafety, "dup", []string{"cli"}), nil); err != nil {
		t.Fatal(err)
	}
	err := r.Add(makeFrame(frames.CategoryGitSafety, "dup", []string{"cli"}), nil)
	if err == nil {
		t.Fatal("expected duplicate-id error, got nil")
	}
}

func TestRegistry_ProjectOverridesStdlib(t *testing.T) {
	r := NewRegistry()
	stdlibFrame := makeFrame(frames.CategoryGitSafety, "ovr", []string{"cli"})
	stdlibFrame.SourcePath = "stdlib:git-safety/ovr.md"
	if err := r.Add(stdlibFrame, nil); err != nil {
		t.Fatal(err)
	}
	projectFrame := makeFrame(frames.CategoryGitSafety, "ovr", []string{"cli", "pre-commit"})
	projectFrame.SourcePath = "/project/.appframes/git-safety/ovr.md"
	if err := r.AddProjectOverride(projectFrame, nil); err != nil {
		t.Fatalf("AddProjectOverride: %v", err)
	}
	matched := r.MatchingTrigger("pre-commit")
	if len(matched) != 1 {
		t.Errorf("pre-commit matched = %v, want [ovr]", names(matched))
	}
}

func names(reg []RegisteredFrame) []string {
	var out []string
	for _, r := range reg {
		out = append(out, r.Frame.Frontmatter.Name)
	}
	return out
}
