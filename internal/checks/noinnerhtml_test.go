// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"testing"

	"nimblegate/internal/engine"
)

func writeJS(t *testing.T, root, rel, content string) string {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNoInnerHTML_DetectsAssignment(t *testing.T) {
	root := t.TempDir()
	writeJS(t, root, "app.js", "el.innerHTML = userInput;\n")
	got := NoInnerHTMLUserInput(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ChangedFiles: []string{filepath.Join(root, "app.js")},
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK; reason = %q", got.Outcome, got.Reason)
	}
}

func TestNoInnerHTML_LiteralStringPasses(t *testing.T) {
	root := t.TempDir()
	writeJS(t, root, "app.js", `el.innerHTML = "";`+"\n")
	got := NoInnerHTMLUserInput(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ChangedFiles: []string{filepath.Join(root, "app.js")},
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS for literal string assignment; reason = %q", got.Outcome, got.Reason)
	}
}

func TestNoInnerHTML_NoChangedFilesScansEntireProject(t *testing.T) {
	root := t.TempDir()
	writeJS(t, root, "src/x.js", "el.innerHTML = userInput;\n")
	got := NoInnerHTMLUserInput(engine.CheckContext{
		Trigger:     engine.TriggerCLI,
		ProjectRoot: root,
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK on project-wide scan", got.Outcome)
	}
}

func TestNoInnerHTML_DisableCommentSuppresses(t *testing.T) {
	root := t.TempDir()
	writeJS(t, root, "app.js", "// appframes:disable security/no-innerHTML-user-input\nel.innerHTML = userInput;\n")
	got := NoInnerHTMLUserInput(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ChangedFiles: []string{filepath.Join(root, "app.js")},
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (disable comment present)", got.Outcome)
	}
}
