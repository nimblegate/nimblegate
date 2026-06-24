// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func writeCodeDocMap(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, ".appframes", "_canonical")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "code-doc-map.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDocTouchesWithCode_StagedSourceWithoutDocFires - the headline case
// the frame is built for.
func TestDocTouchesWithCode_StagedSourceWithoutDocFires(t *testing.T) {
	root := t.TempDir()
	writeCodeDocMap(t, root, `
[code-to-docs]
"internal/checks/*.go" = "docs/frame-authoring.md"
`)
	writeSource(t, root, "internal/checks/folderbranchlock.go", "package checks\n")
	got := DocTouchesWithCode(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{filepath.Join(root, "internal/checks/folderbranchlock.go")},
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s, want WARN; reason = %q", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "internal/checks/folderbranchlock.go") {
		t.Errorf("reason missing source path: %s", got.Reason)
	}
	if !strings.Contains(got.Reason, "docs/frame-authoring.md") {
		t.Errorf("reason missing expected doc: %s", got.Reason)
	}
}

// TestDocTouchesWithCode_StagedSourceWithMappedDocPasses
func TestDocTouchesWithCode_StagedSourceWithMappedDocPasses(t *testing.T) {
	root := t.TempDir()
	writeCodeDocMap(t, root, `
[code-to-docs]
"internal/checks/*.go" = "docs/frame-authoring.md"
`)
	writeSource(t, root, "internal/checks/folderbranchlock.go", "package checks\n")
	writeSource(t, root, "docs/frame-authoring.md", "# Frame authoring\n")
	got := DocTouchesWithCode(engine.CheckContext{
		Trigger:     engine.TriggerPreCommit,
		ProjectRoot: root,
		ChangedFiles: []string{
			filepath.Join(root, "internal/checks/folderbranchlock.go"),
			filepath.Join(root, "docs/frame-authoring.md"),
		},
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (doc was staged alongside source); reason = %q",
			got.Outcome, got.Reason)
	}
}

// TestDocTouchesWithCode_NoCanonicalTableSkips
func TestDocTouchesWithCode_NoCanonicalTableSkips(t *testing.T) {
	root := t.TempDir()
	got := DocTouchesWithCode(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{filepath.Join(root, "src/foo.go")},
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeSkip {
		t.Errorf("outcome = %s, want SKIP (no canonical table)", got.Outcome)
	}
}

// TestDocTouchesWithCode_NoStagedMatchPasses - staged files exist but
// none match any mapping glob.
func TestDocTouchesWithCode_NoStagedMatchPasses(t *testing.T) {
	root := t.TempDir()
	writeCodeDocMap(t, root, `
[code-to-docs]
"internal/checks/*.go" = "docs/frame-authoring.md"
`)
	writeSource(t, root, "unrelated/script.sh", "#!/bin/bash\necho hi\n")
	got := DocTouchesWithCode(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{filepath.Join(root, "unrelated/script.sh")},
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (no glob matched); reason = %q",
			got.Outcome, got.Reason)
	}
}

// TestDocTouchesWithCode_CLIEmptyChangedFilesSkips - cli without staged
// set has no useful answer; SKIP rather than guess.
func TestDocTouchesWithCode_CLIEmptyChangedFilesSkips(t *testing.T) {
	root := t.TempDir()
	writeCodeDocMap(t, root, `
[code-to-docs]
"internal/checks/*.go" = "docs/frame-authoring.md"
`)
	got := DocTouchesWithCode(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ChangedFiles: nil,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeSkip {
		t.Errorf("outcome = %s, want SKIP (cli + no staged set)", got.Outcome)
	}
}

// TestDocTouchesWithCode_PreCommitEmptyChangedFilesPasses - file-scan scope contract.
func TestDocTouchesWithCode_PreCommitEmptyChangedFilesPasses(t *testing.T) {
	root := t.TempDir()
	writeCodeDocMap(t, root, `
[code-to-docs]
"internal/checks/*.go" = "docs/frame-authoring.md"
`)
	got := DocTouchesWithCode(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: nil,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (pre-commit + empty stage)", got.Outcome)
	}
}

// TestDocTouchesWithCode_PerFileDisableSuppresses
func TestDocTouchesWithCode_PerFileDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeCodeDocMap(t, root, `
[code-to-docs]
"internal/checks/*.go" = "docs/frame-authoring.md"
`)
	writeSource(t, root, "internal/checks/folderbranchlock.go",
		"// appframes:disable documentation/doc-touches-with-code\npackage checks\n")
	got := DocTouchesWithCode(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{filepath.Join(root, "internal/checks/folderbranchlock.go")},
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (per-file disable suppresses); reason = %q",
			got.Outcome, got.Reason)
	}
}

// TestDocTouchesWithCode_MultipleMappings - exercises the glob-loop over
// multiple [code-to-docs] entries.
func TestDocTouchesWithCode_MultipleMappings(t *testing.T) {
	root := t.TempDir()
	writeCodeDocMap(t, root, `
[code-to-docs]
"internal/checks/*.go" = "docs/frame-authoring.md"
"cmd/nimblegate/main.go" = "README.md"
"internal/canonical/*.go" = "docs/canonical-tables.md"
`)
	writeSource(t, root, "cmd/nimblegate/main.go", "package main\n")
	got := DocTouchesWithCode(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{filepath.Join(root, "cmd/nimblegate/main.go")},
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s, want WARN", got.Outcome)
	}
	if !strings.Contains(got.Reason, "cmd/nimblegate/main.go") || !strings.Contains(got.Reason, "README.md") {
		t.Errorf("reason missing expected mapping; got: %s", got.Reason)
	}
}

// TestDocTouchesWithCode_EmptyMappingSkips
func TestDocTouchesWithCode_EmptyMappingSkips(t *testing.T) {
	root := t.TempDir()
	writeCodeDocMap(t, root, `
[code-to-docs]
`)
	got := DocTouchesWithCode(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{filepath.Join(root, "src/foo.go")},
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeSkip {
		t.Errorf("outcome = %s, want SKIP (empty mapping)", got.Outcome)
	}
}

// TestDocTouchesWithCode_MissingSection
func TestDocTouchesWithCode_MissingSection(t *testing.T) {
	root := t.TempDir()
	writeCodeDocMap(t, root, `
[unrelated]
foo = "bar"
`)
	got := DocTouchesWithCode(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{filepath.Join(root, "src/foo.go")},
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeSkip {
		t.Errorf("outcome = %s, want SKIP (no [code-to-docs] section)", got.Outcome)
	}
}
