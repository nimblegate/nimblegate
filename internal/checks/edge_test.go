// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"nimblegate/internal/engine"
)

// TestFolderBranchLock_NestedSubdirUsesFirstSegment - current impl takes
// first path segment as the leaf. A working dir deep inside infra/sub/sub2
// should still match the "infra/" entry.
func TestFolderBranchLock_NestedSubdirUsesFirstSegment(t *testing.T) {
	root := t.TempDir()
	writeFolderBranchMap(t, root, `
[folders]
"infra/" = "infra"
`)
	if err := os.MkdirAll(filepath.Join(root, "infra", "sub", "sub2"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := engine.CheckContext{
		Trigger:       engine.TriggerGitWrap,
		ProjectRoot:   root,
		WorkingDir:    filepath.Join(root, "infra", "sub", "sub2"),
		CurrentBranch: "infra",
	}
	got := FolderBranchLock(ctx)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (deep subdir under infra/); reason = %q", got.Outcome, got.Reason)
	}
}

// TestFolderBranchLock_ProjectRootEqualsWorkingDir - when WorkingDir is the
// project root itself, the leaf becomes "./" - must match an explicit entry.
func TestFolderBranchLock_ProjectRootMatchesDotSlashEntry(t *testing.T) {
	root := t.TempDir()
	writeFolderBranchMap(t, root, `
[folders]
"./" = "main"
`)
	ctx := engine.CheckContext{
		Trigger:       engine.TriggerGitWrap,
		ProjectRoot:   root,
		WorkingDir:    root,
		CurrentBranch: "main",
	}
	got := FolderBranchLock(ctx)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; reason = %q", got.Outcome, got.Reason)
	}
}

// TestNoForcePushMain_EmptyCommandSkips
func TestNoForcePushMain_EmptyCommandSkips(t *testing.T) {
	got := NoForcePushMain(engine.CheckContext{Command: ""})
	if got.Outcome != engine.OutcomeSkip {
		t.Errorf("empty command outcome = %s", got.Outcome)
	}
}

// TestNoForcePushMain_WhitespaceCommandSkips
func TestNoForcePushMain_WhitespaceCommandSkips(t *testing.T) {
	got := NoForcePushMain(engine.CheckContext{Command: "   "})
	if got.Outcome != engine.OutcomeSkip {
		t.Errorf("whitespace command outcome = %s", got.Outcome)
	}
}

// TestNoForcePushMain_BranchNameContainsProtectedSubstring - branch
// "main-rewrite" must NOT trigger the protected-branch BLOCK (currently the
// check does exact-string compare, so this is safe; document it).
func TestNoForcePushMain_BranchNameSubstring(t *testing.T) {
	got := NoForcePushMain(engine.CheckContext{Command: "git push --force origin main-rewrite"})
	if got.Outcome == engine.OutcomeBlock {
		t.Errorf("substring match incorrectly blocked: %s", got.Reason)
	}
}

// TestNoForcePushMain_ForceWithLeaseToMainStillBlocks - --force-with-lease
// is safer than --force but the current check blocks all three. Document.
func TestNoForcePushMain_ForceWithLeaseToMain(t *testing.T) {
	got := NoForcePushMain(engine.CheckContext{Command: "git push --force-with-lease origin main"})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("--force-with-lease to main outcome = %s, want BLOCK (current impl treats it same as --force)", got.Outcome)
	}
}

// TestAptPurgePreview_ShortFlag - `apt-get remove -s pkg` (using -s for
// simulate) must pass.
func TestAptPurgePreview_ShortSimulateFlag(t *testing.T) {
	got := AptPurgePreview(engine.CheckContext{Command: "apt-get remove -s rpcbind"})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS for -s short flag", got.Outcome)
	}
}

// TestAptPurgePreview_MultipleSpacesInCommand
func TestAptPurgePreview_MultipleSpacesInCommand(t *testing.T) {
	got := AptPurgePreview(engine.CheckContext{Command: "apt    purge   rpcbind"})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("multiple-space command outcome = %s, want BLOCK", got.Outcome)
	}
}

// TestNoInnerHTML_HugeFile - file just under the 1 MiB cap with a
// trailing violation must still be detected. Verifies the scanner runs
// at the practical size limit without missing tail-of-file findings.
func TestNoInnerHTML_HugeFile(t *testing.T) {
	root := t.TempDir()
	var content strings.Builder
	// 14000 lines × ~67 bytes = ~938 KB - comfortably under DefaultMaxFileBytes (1 MiB)
	for i := 0; i < 14000; i++ {
		content.WriteString("// padding line ")
		content.WriteString(strings.Repeat("x", 50))
		content.WriteByte('\n')
	}
	content.WriteString("el.innerHTML = userInput;\n")
	path := filepath.Join(root, "big.js")
	if err := os.WriteFile(path, []byte(content.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := NoInnerHTMLUserInput(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ChangedFiles: []string{path},
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s for big file with trailing violation", got.Outcome)
	}
}

// TestNoInnerHTML_OversizedFileSkipped - verifies the security size cap.
// A file just over DefaultMaxFileBytes containing a violation is
// deliberately skipped (OOM-DoS prevention). The trade-off is acknowledged:
// pushing a >1 MiB file with a buried innerHTML violation bypasses this
// frame, but the alternative (unbounded scan) is a real OOM vector.
// Operators who need to scan larger files raise the cap intentionally.
func TestNoInnerHTML_OversizedFileSkipped(t *testing.T) {
	root := t.TempDir()
	// DefaultMaxFileBytes + 1 - minimum size to trigger the cap.
	big := make([]byte, DefaultMaxFileBytes+1)
	copy(big, []byte("el.innerHTML = userInput;\n"))
	path := filepath.Join(root, "oversized.js")
	if err := os.WriteFile(path, big, 0o644); err != nil {
		t.Fatal(err)
	}
	got := NoInnerHTMLUserInput(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ChangedFiles: []string{path},
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s for oversized file; want PASS (size-cap skip)", got.Outcome)
	}
}

// TestNoInnerHTML_BinaryFileWithDotJsExtension - pretend-binary file shouldn't
// crash the scan.
func TestNoInnerHTML_BinaryWithJSExt(t *testing.T) {
	root := t.TempDir()
	binary := make([]byte, 4096)
	for i := range binary {
		binary[i] = byte(i % 256)
	}
	path := filepath.Join(root, "bin.js")
	if err := os.WriteFile(path, binary, 0o644); err != nil {
		t.Fatal(err)
	}
	got := NoInnerHTMLUserInput(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ChangedFiles: []string{path},
	})
	// Outcome doesn't matter much; must not panic or hang.
	if got.Outcome != engine.OutcomePass && got.Outcome != engine.OutcomeBlock {
		t.Errorf("binary-as-JS outcome = %s (acceptable: PASS or BLOCK, not other)", got.Outcome)
	}
}

// TestNoInnerHTML_NonexistentFileInChangedFiles - list contains a file that
// doesn't exist; scan should silently skip it.
func TestNoInnerHTML_NonexistentChangedFile(t *testing.T) {
	got := NoInnerHTMLUserInput(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  t.TempDir(),
		ChangedFiles: []string{"/does/not/exist.js"},
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s for nonexistent changed file (want PASS - silent skip)", got.Outcome)
	}
}

// TestNoInnerHTML_ManyFilesInChangedList - 1000 files, half with violation.
func TestNoInnerHTML_ManyChangedFiles(t *testing.T) {
	root := t.TempDir()
	var files []string
	for i := 0; i < 1000; i++ {
		var content string
		if i%2 == 0 {
			content = "el.innerHTML = userInput;\n"
		} else {
			content = "el.textContent = 'ok';\n"
		}
		path := filepath.Join(root, fmt.Sprintf("f%04d.js", i))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, path)
	}
	got := NoInnerHTMLUserInput(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ChangedFiles: files,
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
}

// TestCrossBranchID_DuplicateIDsInTable - two domains pointing to the same
// ID. The valid-set is a map keyed by ID value, so duplicates collapse.
func TestCrossBranchID_DuplicateIDsInTable(t *testing.T) {
	root := t.TempDir()
	writeWebsiteIDsTable(t, root, `
[ids]
"a.com" = "shared"
"b.com" = "shared"
`)
	writeFile(t, root, "page.html", `<script data-website-id="shared"></script>`)
	got := CrossBranchIDConsistency(engine.CheckContext{
		Trigger:     engine.TriggerCLI,
		ProjectRoot: root,
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (shared id in valid set)", got.Outcome)
	}
}

// TestCrossBranchID_NestedDirIsScanned - IDs inside subdirs must also be
// checked (current walker descends).
func TestCrossBranchID_ScansSubdirs(t *testing.T) {
	root := t.TempDir()
	writeWebsiteIDsTable(t, root, `
[ids]
"a.com" = "expected"
`)
	writeFile(t, root, "deep/sub/dir/page.html", `<script data-website-id="wrong"></script>`)
	got := CrossBranchIDConsistency(engine.CheckContext{
		Trigger:     engine.TriggerCLI,
		ProjectRoot: root,
	})
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s, want WARN (wrong id in deep subdir)", got.Outcome)
	}
}

// TestChecks_ConcurrentInvocations - confirm each check function is
// re-entrant when called from many goroutines simultaneously (the engine's
// parallel runner already exercises this in practice).
func TestChecks_ConcurrentInvocations(t *testing.T) {
	root := t.TempDir()
	writeWebsiteIDsTable(t, root, `
[ids]
"a.com" = "abc"
`)
	writeFolderBranchMap(t, root, `
[folders]
"./" = "main"
`)
	writeFile(t, root, "page.html", `<script data-website-id="abc"></script>`)
	writeFile(t, root, "app.js", "el.textContent = 'ok';\n")

	checks := []func() engine.CheckResult{
		func() engine.CheckResult {
			return FolderBranchLock(engine.CheckContext{
				Trigger: engine.TriggerGitWrap, ProjectRoot: root, WorkingDir: root, CurrentBranch: "main",
			})
		},
		func() engine.CheckResult {
			return NoForcePushMain(engine.CheckContext{Command: "git push origin main"})
		},
		func() engine.CheckResult {
			return AptPurgePreview(engine.CheckContext{Command: "apt install rpcbind"})
		},
		func() engine.CheckResult {
			return NoInnerHTMLUserInput(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root})
		},
		func() engine.CheckResult {
			return CrossBranchIDConsistency(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root})
		},
	}

	var wg sync.WaitGroup
	for round := 0; round < 50; round++ {
		for _, fn := range checks {
			fn := fn
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = fn()
			}()
		}
	}
	wg.Wait()
}
