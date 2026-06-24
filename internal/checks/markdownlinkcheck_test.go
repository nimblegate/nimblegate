// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func writeMD(t *testing.T, root, rel, content string) string {
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

func writeIgnoreTable(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, ".appframes", "_canonical")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "markdown-link-ignore.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestMarkdownLinkCheck_BrokenLinkFires
func TestMarkdownLinkCheck_BrokenLinkFires(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "INDEX.md", "See [the bad](./does-not-exist.md) for details.\n")
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s, want WARN; reason = %q", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "does-not-exist.md") {
		t.Errorf("reason missing link target: %s", got.Reason)
	}
}

// TestMarkdownLinkCheck_ValidRelativeLinkPasses
func TestMarkdownLinkCheck_ValidRelativeLinkPasses(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "docs/INDEX.md", "See [target](./target.md).\n")
	writeMD(t, root, "docs/target.md", "# Target\n")
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS; reason = %q", got.Outcome, got.Reason)
	}
}

// TestMarkdownLinkCheck_ParentRelativeLink
func TestMarkdownLinkCheck_ParentRelativeLink(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "docs/sub/INDEX.md", "See [up](../target.md).\n")
	writeMD(t, root, "docs/target.md", "# Target\n")
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS for ../ resolution", got.Outcome)
	}
}

// TestMarkdownLinkCheck_AnchorOnlyLinkSkipped
func TestMarkdownLinkCheck_AnchorOnlyLinkSkipped(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "doc.md", "Jump to [heading](#some-heading) below.\n")
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS for pure-anchor link", got.Outcome)
	}
}

// TestMarkdownLinkCheck_StripsAnchorBeforeResolving
func TestMarkdownLinkCheck_StripsAnchorBeforeResolving(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "INDEX.md", "[target](./target.md#section-2)\n")
	writeMD(t, root, "target.md", "# Target\n")
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (anchor should be stripped)", got.Outcome)
	}
}

// TestMarkdownLinkCheck_ExternalSchemesSkipped - http, https, mailto, tel,
// data, javascript should not be validated.
func TestMarkdownLinkCheck_ExternalSchemesSkipped(t *testing.T) {
	root := t.TempDir()
	body := strings.Join([]string{
		"[a](https://example.com/page)",
		"[b](http://example.com)",
		"[c](mailto:alice@example.com)",
		"[d](tel:+15551234)",
		"[e](data:text/html;base64,Zm9v)",
		"[f](javascript:alert(1))",
		"",
	}, "\n")
	writeMD(t, root, "INDEX.md", body)
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (external schemes skipped); reason = %q",
			got.Outcome, got.Reason)
	}
}

// TestMarkdownLinkCheck_IgnoredPrefixSuppresses - the orphan-branch
// escape hatch.
func TestMarkdownLinkCheck_IgnoredPrefixSuppresses(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "INDEX.md",
		"See [marketing](./marketing/CLAUDE.md) and [infra](./infra/CLAUDE.md).\n")
	writeIgnoreTable(t, root, `
[ignored-prefixes]
"marketing/" = "orphan-branch"
"infra/" = "orphan-branch"
`)
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (ignored prefixes); reason = %q",
			got.Outcome, got.Reason)
	}
}

// TestMarkdownLinkCheck_IgnoredPrefixDoesntHideRealBroken - links that
// AREN'T in the ignore set still fire.
func TestMarkdownLinkCheck_IgnoredPrefixDoesntHideRealBroken(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "INDEX.md",
		"See [marketing](./marketing/CLAUDE.md) and [real-broken](./typo.md).\n")
	writeIgnoreTable(t, root, `
[ignored-prefixes]
"marketing/" = "orphan-branch"
`)
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s, want WARN - typo.md is genuinely broken", got.Outcome)
	}
	if !strings.Contains(got.Reason, "typo.md") {
		t.Errorf("reason missing typo.md: %s", got.Reason)
	}
	if strings.Contains(got.Reason, "marketing/CLAUDE.md") {
		t.Errorf("marketing/ link leaked through ignore: %s", got.Reason)
	}
}

// TestMarkdownLinkCheck_PerFileDisableSuppresses
func TestMarkdownLinkCheck_PerFileDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "INDEX.md",
		"<!-- appframes:disable documentation/markdown-link-check-internal -->\nSee [broken](./never.md).\n")
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (per-file disable); reason = %q",
			got.Outcome, got.Reason)
	}
}

// TestMarkdownLinkCheck_ImageSyntax - !\[alt\](path) is checked the same way.
func TestMarkdownLinkCheck_ImageSyntax(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "INDEX.md", "![logo](./missing-logo.png)\n")
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s, want WARN for missing image", got.Outcome)
	}
}

// TestMarkdownLinkCheck_AbsoluteRootRelative - `/foo/bar.md` is treated
// as project-root-relative (markdown convention).
func TestMarkdownLinkCheck_AbsoluteRootRelative(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "docs/sub/index.md", "[link](/target.md)\n")
	writeMD(t, root, "target.md", "# Target\n")
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (root-relative resolution)", got.Outcome)
	}
}

// TestMarkdownLinkCheck_NoiseDirsExcluded
func TestMarkdownLinkCheck_NoiseDirsExcluded(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "node_modules/dep/README.md", "[broken](./does-not-exist.md)\n")
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (node_modules excluded)", got.Outcome)
	}
}

// TestMarkdownLinkCheck_PreCommitEmptyChangesPasses - file-scan scope contract.
func TestMarkdownLinkCheck_PreCommitEmptyChangesPasses(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "INDEX.md", "[broken](./missing.md)\n")
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: nil,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS - pre-commit + empty stage", got.Outcome)
	}
}

// TestMarkdownLinkCheck_OutOfProjectPathSkipped - `../../../etc/passwd`
// would escape the project root; we don't validate those at all.
func TestMarkdownLinkCheck_OutOfProjectPathSkipped(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "INDEX.md", "[evil](../../../etc/passwd)\n")
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (paths escaping root are not validated)", got.Outcome)
	}
}

// TestMarkdownLinkCheck_InlineCodeSpanSkipped - links inside `inline code`
// are syntax examples, not real links; per CommonMark they aren't parsed.
func TestMarkdownLinkCheck_InlineCodeSpanSkipped(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "DOC.md", "Users can type `[link](url)` or `[a](./nope.md)` directly in fields.\n")
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (links inside inline code); reason = %q", got.Outcome, got.Reason)
	}
}

// TestMarkdownLinkCheck_FencedBlockSkipped - links inside a ``` fenced block
// are illustrative, not real links.
func TestMarkdownLinkCheck_FencedBlockSkipped(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "DOC.md", "Example:\n\n```\nSee [x](./does-not-exist.md) and ![img](/img.jpg)\n```\n\nDone.\n")
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (links inside fenced block); reason = %q", got.Outcome, got.Reason)
	}
}

// TestMarkdownLinkCheck_RealLinkAfterFenceStillCaught - fence state must
// reset, so a real broken link after the closing fence still fires; and the
// link inside the fence must NOT fire.
func TestMarkdownLinkCheck_RealLinkAfterFenceStillCaught(t *testing.T) {
	root := t.TempDir()
	writeMD(t, root, "DOC.md", "```\n[x](./fenced-nope.md)\n```\n\nReal [broken](./real-nope.md) link.\n")
	got := MarkdownLinkCheckInternal(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s, want WARN (real link after fence); reason = %q", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "real-nope.md") {
		t.Errorf("expected the post-fence broken link to fire; reason = %q", got.Reason)
	}
	if strings.Contains(got.Reason, "fenced-nope.md") {
		t.Errorf("link inside fenced block should NOT fire; reason = %q", got.Reason)
	}
}
