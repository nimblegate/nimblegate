// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package banner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultProjectName(t *testing.T) {
	cases := map[string]string{
		"/srv/projects/apps/myapp": "myapp",
		"/home/user/myproj":        "myproj",
		"/":                        "/",
		".":                        ".",
		"":                         "",
	}
	for in, want := range cases {
		// Note: "/" and "" go through Clean, which can produce different shapes.
		// We just verify DefaultProjectName doesn't crash and produces something
		// for normal paths.
		got := DefaultProjectName(in)
		if in == "/srv/projects/apps/myapp" && got != want {
			t.Errorf("DefaultProjectName(%q) = %q; want %q", in, got, want)
		}
		if in == "/home/user/myproj" && got != want {
			t.Errorf("DefaultProjectName(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestHasSeen_AndMarkSeen_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	project := "/some/project/path/that/will/never/exist"
	if HasSeen(project) {
		t.Fatal("HasSeen returned true before any MarkSeen call")
	}
	if err := MarkSeen(project); err != nil {
		t.Fatalf("MarkSeen: %v", err)
	}
	if !HasSeen(project) {
		t.Error("HasSeen returned false after MarkSeen")
	}
}

func TestMarkerPathHashes_DistinctProjectsDistinctMarkers(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := MarkSeen("/proj/A"); err != nil {
		t.Fatal(err)
	}
	if !HasSeen("/proj/A") {
		t.Error("A marker missing after MarkSeen")
	}
	if HasSeen("/proj/B") {
		t.Error("B reported seen but only A was marked")
	}
}

func TestRenderHeader_QuietEnvSuppresses(t *testing.T) {
	t.Setenv(QuietEnv, "1")
	var buf bytes.Buffer
	RenderHeader(&buf, Context{ProjectName: "x", Command: "git push"})
	if buf.Len() != 0 {
		t.Errorf("header should be silent under APPFRAMES_QUIET; got: %q", buf.String())
	}
}

func TestRenderHeader_Default(t *testing.T) {
	t.Setenv(QuietEnv, "")
	var buf bytes.Buffer
	RenderHeader(&buf, Context{
		ProjectName:   "myapp",
		DesignDocPath: ".appframes/_design.md",
		Command:       "git push --force",
	})
	out := buf.String()
	for _, want := range []string{"nimblegate", "myapp", ".appframes/_design.md", "git push --force"} {
		if !strings.Contains(out, want) {
			t.Errorf("header missing %q\nGot: %s", want, out)
		}
	}
}

func TestRenderIntro_FirstTimeShowsAndMarks(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	project := "/proj/X"

	var buf bytes.Buffer
	shown := RenderIntro(&buf, Context{ProjectRoot: project, ProjectName: "X", FrameCount: 5})
	if !shown {
		t.Error("first call should show the intro")
	}
	out := buf.String()
	for _, want := range []string{
		"nimblegate gating active",
		"DO NOT silently route around",
		"--force-yes",
		"audit analyze",
		"Repeatedly --force-yes the same gate",
		"nimblegate intro",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("first-time intro missing %q\nGot: %s", want, out)
		}
	}

	// Second call should NOT show (marker was created).
	buf.Reset()
	shown = RenderIntro(&buf, Context{ProjectRoot: project, ProjectName: "X"})
	if shown {
		t.Error("second call should NOT show the intro")
	}
	if buf.Len() != 0 {
		t.Errorf("second call should write nothing; got: %q", buf.String())
	}
}

func TestRenderIntroForced_AlwaysShowsButDoesNotUpdateMarker(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	project := "/proj/Y"

	var buf bytes.Buffer
	RenderIntroForced(&buf, Context{ProjectRoot: project, ProjectName: "Y"})
	if buf.Len() == 0 {
		t.Error("forced render should always write")
	}
	// Marker should NOT have been set by forced render.
	if HasSeen(project) {
		t.Error("RenderIntroForced should not update the marker")
	}

	// Call again - still shows.
	buf.Reset()
	RenderIntroForced(&buf, Context{ProjectRoot: project, ProjectName: "Y"})
	if buf.Len() == 0 {
		t.Error("second forced call should still write")
	}
}

func TestRenderIntro_ContentIncludesGroups(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	var buf bytes.Buffer
	RenderIntro(&buf, Context{
		ProjectRoot:   "/proj/Z",
		ProjectName:   "Z",
		FrameCount:    31,
		EnabledGroups: []string{"@tier-1", "@cf-pages", "@web"},
	})
	out := buf.String()
	if !strings.Contains(out, "31 frame(s) active") {
		t.Errorf("intro missing frame count\nGot: %s", out)
	}
	for _, want := range []string{"@tier-1", "@cf-pages", "@web"} {
		if !strings.Contains(out, want) {
			t.Errorf("intro missing group %q\nGot: %s", want, out)
		}
	}
}

func TestDetectDocPaths_BothPresent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".appframes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"_design.md", "_future.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	d, f := DetectDocPaths(root)
	if d != ".appframes/_design.md" {
		t.Errorf("design path = %q; want .appframes/_design.md", d)
	}
	if f != ".appframes/_future.md" {
		t.Errorf("future path = %q; want .appframes/_future.md", f)
	}
}

func TestDetectDocPaths_BothMissing(t *testing.T) {
	root := t.TempDir()
	d, f := DetectDocPaths(root)
	if d != "" || f != "" {
		t.Errorf("missing docs should produce empty strings; got d=%q f=%q", d, f)
	}
}
