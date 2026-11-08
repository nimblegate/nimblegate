// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
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

func TestNoInnerHTML_CodeThatDoesNotRunPasses(t *testing.T) {
	pass := map[string]string{
		"public/docs/frames/index.html": "<span class=\"fd\">Detect element.innerHTML = &lt;expression patterns</span>\n<pre><code>el.innerHTML = x;</code></pre>\n",
		"src/notes.js":                  "// classic .innerHTML = identifier pattern\n/* el.innerHTML = y */\n",
		"vendor/jspdf.umd.min.js":       "a.innerHTML=b;\n",
		"lib/pixi.js":                   strings.Repeat("x=1;", 300) + "e.innerHTML=t;\n",
		"testdata/positives/a.js":       "el.innerHTML = userInput;\n",
	}
	for rel, body := range pass {
		root := t.TempDir()
		writeJS(t, root, rel, body)
		got := NoInnerHTMLUserInput(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
		if got.Outcome != engine.OutcomePass {
			t.Errorf("%s: want PASS, got %s %s", rel, got.Outcome, got.Reason)
		}
	}
	root := t.TempDir()
	writeJS(t, root, "site/index.html", "<p>docs</p>\n<script>\n  aside.innerHTML = html;\n</script>\n")
	got := NoInnerHTMLUserInput(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome == engine.OutcomePass || len(got.Hits) != 1 || got.Hits[0].Line != 3 {
		t.Errorf("assignment inside <script> must fire on line 3; got %s %+v", got.Outcome, got.Hits)
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
