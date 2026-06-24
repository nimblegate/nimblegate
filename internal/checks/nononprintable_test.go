// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"testing"

	"nimblegate/internal/engine"
)

func runNonPrintableCheck(t *testing.T, rel, content string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, content)
	return NoNonPrintable(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestNoNonPrintable_ESCByteWarns(t *testing.T) {
	body := "foo\x1b[31mbar\x1b[0m\n"
	got := runNonPrintableCheck(t, "x.txt", body)
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s, want WARN", got.Outcome)
	}
}

func TestNoNonPrintable_NULByteWarns(t *testing.T) {
	body := "x\x00y\n"
	got := runNonPrintableCheck(t, "x.txt", body)
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s, want WARN", got.Outcome)
	}
}

func TestNoNonPrintable_TabsLFsCRsPass(t *testing.T) {
	body := "line\twith\ttabs\nand a CR\r\nfine\n"
	got := runNonPrintableCheck(t, "x.txt", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("tabs/LFs/CRs should pass; got %s reason=%s", got.Outcome, got.Reason)
	}
}

func TestNoNonPrintable_BinaryExtensionSkipped(t *testing.T) {
	body := "\x00\x01\x02binary content\x1b\n"
	got := runNonPrintableCheck(t, "logo.png", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf(".png should be skipped; got %s", got.Outcome)
	}
}
