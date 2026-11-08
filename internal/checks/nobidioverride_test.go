// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

// This file is the frame's own fixture corpus, so the frame must not
// report on it. Until the matcher required a standalone line, a marker
// quoted inside a fixture suppressed this file by accident rather than
// by decision; the decision is now on the record.
// appframes:disable security/no-bidi-override

func runBidiCheck(t *testing.T, rel, content string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, content)
	return NoBidiOverride(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

// TestNoBidiOverride_BinaryFilesSkipped: compressed and image bytes hold
// bidi byte sequences by chance. A binary extension or a NUL byte skips the
// file; the same bytes in a text file still fire.
func TestNoBidiOverride_BinaryFilesSkipped(t *testing.T) {
	noise := "\x89PNG\r\n\x1a\nIDAT\xe2\x80\xae\x13\x07"
	for rel, body := range map[string]string{
		"img/photo.png":     noise,
		"video/clip.mp4":    noise,
		"assets/app.js.br":  noise,
		"sumdb/tile/8/x926": "\x00\x17" + noise,
	} {
		if got := runBidiCheck(t, rel, body); got.Outcome != engine.OutcomePass {
			t.Errorf("%s: outcome = %s, want PASS; reason=%s", rel, got.Outcome, got.Reason)
		}
	}
	if got := runBidiCheck(t, "app.js", "x = 1\xe2\x80\xae\n"); got.Outcome != engine.OutcomeBlock {
		t.Errorf("text file: outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoBidiOverride_RLOBlocks(t *testing.T) {
	// U+202E (RLO) embedded mid-line - the classic Trojan Source pattern.
	body := "if (admin)‮ { grant() } else {}\n"
	got := runBidiCheck(t, "app.go", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
	if !strings.Contains(got.Reason, "U+202E") {
		t.Errorf("reason should name the codepoint: %s", got.Reason)
	}
}

func TestNoBidiOverride_LREBlocks(t *testing.T) {
	body := "x = ‪comment\n"
	got := runBidiCheck(t, "lib/util.py", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoBidiOverride_PDIBlocks(t *testing.T) {
	body := "foo⁩bar\n"
	got := runBidiCheck(t, "main.js", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoBidiOverride_CleanPasses(t *testing.T) {
	body := "if (admin) { grant() }\n"
	got := runBidiCheck(t, "app.go", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS; reason: %s", got.Outcome, got.Reason)
	}
}

func TestNoBidiOverride_FileLevelMarkerSuppresses(t *testing.T) {
	body := "// appframes:disable security/no-bidi-override\nif (admin)‮\n"
	got := runBidiCheck(t, "app.go", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("file-level marker should suppress; outcome=%s reason=%s", got.Outcome, got.Reason)
	}
}

func TestNoBidiOverride_NextLineMarkerSuppresses(t *testing.T) {
	body := "// appframes:disable-next-line security/no-bidi-override\nif (admin)‮\n"
	got := runBidiCheck(t, "app.go", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("next-line marker should suppress; outcome=%s reason=%s", got.Outcome, got.Reason)
	}
}

func TestNoBidiOverride_PreCommitEmptyPasses(t *testing.T) {
	got := NoBidiOverride(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ChangedFiles: nil,
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("pre-commit + no changed files should PASS; got %s", got.Outcome)
	}
}

func TestNoBidiOverride_MultipleHitsListAllUpToCap(t *testing.T) {
	// Same character on multiple lines - expect each as a separate hit.
	body := "a‮b\nc‮d\ne‮f\n"
	got := runBidiCheck(t, "x.py", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
	if len(got.Hits) != 3 {
		t.Errorf("hits = %d, want 3: %#v", len(got.Hits), got.Hits)
	}
}
