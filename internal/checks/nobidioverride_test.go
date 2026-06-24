// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

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
