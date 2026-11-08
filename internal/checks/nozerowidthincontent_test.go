// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"testing"

	"nimblegate/internal/engine"
)

func runZeroWidthContentCheck(t *testing.T, rel, content string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, content)
	return NoZeroWidthInContent(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestNoZeroWidthInContent_ZWSPInMarkdownWarns(t *testing.T) {
	body := "This has a \u200bsneaky char.\n"
	got := runZeroWidthContentCheck(t, "README.md", body)
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s, want WARN", got.Outcome)
	}
}

func TestNoZeroWidthInContent_CodeSpanExampleIgnored(t *testing.T) {
	body := "Parsers treat `varname` and `var\u200cname` as different.\n\n```\nvar\u200bname := 1\n```\n"
	if got := runZeroWidthContentCheck(t, "docs/attack.md", body); got.Outcome != engine.OutcomePass {
		t.Errorf("zero-width inside code examples should pass; got %s %s", got.Outcome, got.Reason)
	}
	if got := runZeroWidthContentCheck(t, "docs/prose.md", "Hidden in\u200b prose.\n"); got.Outcome != engine.OutcomeWarn {
		t.Errorf("zero-width in prose should warn; got %s", got.Outcome)
	}
}

func TestNoZeroWidthInContent_LeadingBOMIgnored(t *testing.T) {
	body := "\uFEFF# Heading\n"
	got := runZeroWidthContentCheck(t, "README.md", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("leading BOM should be ignored; got %s reason=%s", got.Outcome, got.Reason)
	}
}

func TestNoZeroWidthInContent_LICENSEFileMatched(t *testing.T) {
	body := "MIT License\n\nCopyright\u200b ...\n"
	got := runZeroWidthContentCheck(t, "LICENSE", body)
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("LICENSE should be in scope; got %s reason=%s", got.Outcome, got.Reason)
	}
}

func TestNoZeroWidthInContent_GoFileIgnored(t *testing.T) {
	// Source files are out of scope - security/no-zero-width-in-source handles them.
	body := "package main\nvar x = \"\u200b\"\n"
	got := runZeroWidthContentCheck(t, "main.go", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf(".go is out of scope here; got %s", got.Outcome)
	}
}

func TestNoZeroWidthInContent_CleanPasses(t *testing.T) {
	body := "Just regular markdown.\n"
	got := runZeroWidthContentCheck(t, "README.md", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS", got.Outcome)
	}
}
