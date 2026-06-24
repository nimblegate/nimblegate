// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

// writeSource creates a file with the given content and returns its abs path.
func writeSource(t *testing.T, root, rel, content string) string {
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

// TestDatedTodo_NakedTODOFires - the baseline bad case the frame is built for.
func TestDatedTodo_NakedTODOFires(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "src/main.go", "// TODO: rewrite this once we know more\nfunc main() {}\n")
	got := DatedTodo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s, want WARN; reason = %q", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "main.go:1") {
		t.Errorf("reason missing location: %s", got.Reason)
	}
}

// TestDatedTodo_AcceptedForms - all the documented "okay" forms.
func TestDatedTodo_AcceptedForms(t *testing.T) {
	root := t.TempDir()
	contents := []string{
		"// TODO(2026-06-15): switch to streaming parser\n",
		"// FIXME(2026-06-15: ship V0.5): blocked on auth\n",
		"// FIXME(@maintainer): not thread-safe\n",
		"// TODO(acme): per docs\n",
		"// HACK(#142): workaround for upstream bug\n",
		"// TODO(ACME-321: blocked on auth refactor): retry after that ships\n",
	}
	for i, body := range contents {
		writeSource(t, root, filepath.Join("src", "ok"+itoa(i)+".go"), body)
	}
	got := DatedTodo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS for all tagged forms; reason = %q", got.Outcome, got.Reason)
	}
}

// TestDatedTodo_RejectedForms - explicitly bad cases.
func TestDatedTodo_RejectedForms(t *testing.T) {
	root := t.TempDir()
	bad := []string{
		"// TODO: come back to this\n",
		"// FIXME this is broken\n",
		"// XXX old code, delete?\n",
		"# HACK shell-style, no tag\n",
	}
	for i, body := range bad {
		writeSource(t, root, filepath.Join("src", "bad"+itoa(i)+".sh"), body)
	}
	got := DatedTodo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s, want WARN", got.Outcome)
	}
	// All four files should produce a hit.
	for i := range bad {
		want := "bad" + itoa(i) + ".sh:1"
		if !strings.Contains(got.Reason, want) {
			t.Errorf("missing hit for %s in reason: %s", want, got.Reason)
		}
	}
}

// TestDatedTodo_DisableComment - both file-level + per-line opt-outs.
func TestDatedTodo_FileLevelDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "src/quiet.go",
		"// appframes:disable documentation/dated-todo\n// TODO: anything goes after the disable\n")
	got := DatedTodo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (file-level disable should suppress); reason = %q",
			got.Outcome, got.Reason)
	}
}

func TestDatedTodo_PerLineDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "src/quiet.go",
		"// appframes:disable-next-line documentation/dated-todo\n// TODO untagged but ok on this line\n")
	got := DatedTodo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (per-line disable); reason = %q",
			got.Outcome, got.Reason)
	}
}

// TestDatedTodo_NoiseDirsExcluded - same exclusion path as other checks.
func TestDatedTodo_NoiseDirsExcluded(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "node_modules/dep/badge.js", "// TODO: bundled-bad\n")
	got := DatedTodo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (node_modules excluded)", got.Outcome)
	}
}

// TestDatedTodo_PreCommitEmptyChangesPasses - file-scan scope contract.
func TestDatedTodo_PreCommitEmptyChangesPasses(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "src/main.go", "// TODO: naked\n")
	got := DatedTodo(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: nil, // empty stage
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS - pre-commit + empty stage = no scan", got.Outcome)
	}
}

// TestDatedTodo_PreCommitChangedFilesOnly - scans only those.
func TestDatedTodo_PreCommitChangedFilesOnly(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "src/staged.go", "// TODO: naked\n")
	writeSource(t, root, "src/untouched.go", "// TODO: naked too\n")
	got := DatedTodo(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{filepath.Join(root, "src/staged.go")},
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s, want WARN", got.Outcome)
	}
	if !strings.Contains(got.Reason, "src/staged.go") {
		t.Errorf("missing staged hit: %s", got.Reason)
	}
	if strings.Contains(got.Reason, "untouched.go") {
		t.Errorf("untouched file leaked: %s", got.Reason)
	}
}

// TestDatedTodo_MarkdownAndShellSupport - non-source extensions covered.
func TestDatedTodo_MarkdownAndShellSupport(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "docs/notes.md", "# Notes\n\nTODO write more docs\n")
	writeSource(t, root, "scripts/deploy.sh", "#!/bin/bash\n# FIXME password handling\n")
	got := DatedTodo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s, want WARN (both .md + .sh should fire)", got.Outcome)
	}
	for _, want := range []string{"notes.md", "deploy.sh"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("missing hit for %s: %s", want, got.Reason)
		}
	}
}

// TestDatedTodo_TaggedNextToUntaggedOnDifferentLines - only the untagged
// line fires.
func TestDatedTodo_PerLineSurvivalSelective(t *testing.T) {
	root := t.TempDir()
	content := strings.Join([]string{
		"// TODO(2026-06-15): this is fine",
		"// TODO: this is not",
		"// FIXME(@me): also fine",
		"// HACK no tag here",
		"",
	}, "\n")
	writeSource(t, root, "src/mix.go", content)
	got := DatedTodo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s, want WARN", got.Outcome)
	}
	// Only the bare-TODO line + the bare-HACK line should be in hits.
	if !strings.Contains(got.Reason, "mix.go:2") {
		t.Errorf("missing line 2 (bare TODO): %s", got.Reason)
	}
	if !strings.Contains(got.Reason, "mix.go:4") {
		t.Errorf("missing line 4 (bare HACK): %s", got.Reason)
	}
	if strings.Contains(got.Reason, "mix.go:1") || strings.Contains(got.Reason, "mix.go:3") {
		t.Errorf("tagged lines incorrectly hit: %s", got.Reason)
	}
}

// itoa is a tiny helper so we don't import strconv just for tests.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [12]byte
	n := len(buf)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		buf[n] = '-'
	}
	return string(buf[n:])
}
