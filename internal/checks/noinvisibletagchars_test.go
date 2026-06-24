// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func runTagCharCheck(t *testing.T, rel, content string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, content)
	return NoInvisibleTagChars(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestNoInvisibleTagChars_LanguageTagBlocks(t *testing.T) {
	// U+E0001 - Unicode language tag.
	body := "hello \U000E0001 world\n"
	got := runTagCharCheck(t, "README.md", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
	if !strings.Contains(got.Reason, "U+E0001") {
		t.Errorf("reason should name codepoint: %s", got.Reason)
	}
}

func TestNoInvisibleTagChars_TagSpaceBlocks(t *testing.T) {
	// U+E0020 - tag SPACE, common in tag-encoded payloads.
	body := "ok\U000E0020done\n"
	got := runTagCharCheck(t, "notes.md", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoInvisibleTagChars_CancelTagBlocks(t *testing.T) {
	// U+E007F - cancel tag.
	body := "x\U000E007Fy\n"
	got := runTagCharCheck(t, "x.go", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoInvisibleTagChars_CleanPasses(t *testing.T) {
	body := "Just plain ASCII + normal UTF-8 émojis 🎉.\n"
	got := runTagCharCheck(t, "README.md", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS; reason: %s", got.Outcome, got.Reason)
	}
}

func TestNoInvisibleTagChars_FileLevelMarkerSuppresses(t *testing.T) {
	body := "# appframes:disable security/no-invisible-tag-chars\nhello \U000E0001 world\n"
	got := runTagCharCheck(t, "x.md", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("file-level marker should suppress; outcome=%s", got.Outcome)
	}
}
