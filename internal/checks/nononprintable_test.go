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

func TestNoNonPrintable_BinaryContentSkipped(t *testing.T) {
	cases := map[string]string{
		"model.npy":            "\x93NUMPY\x01\x00v\x00{'descr': '<f8'}\n",
		"tile/8/0/119":         "\x01\x9a\xfe\x02\x03\x04\x05\x06\x07\x08\x0e\x0f\x10\x11\x12\x13",
		"cache/abc.meta":       "\x01\x00\x00\x00\x00\x16[\xe2i\x00\x00",
		"site/app.js.br":       "\x1b\x03\x00\xf8\xa5\x40",
		"lib/jspdf.umd.min.js": "var a=\"\xc2\x8a\";\n",
		"testdata/bell.txt":    "ding \x07\n",
		"tests/invalid/control/bare-null.toml": "a = \x00\n",
	}
	for rel, body := range cases {
		if got := runNonPrintableCheck(t, rel, body); got.Outcome != engine.OutcomePass {
			t.Errorf("%s should be skipped as binary/minified; got %s %s", rel, got.Outcome, got.Reason)
		}
	}
}

func TestNoNonPrintable_TextWithStrayControlStillWarns(t *testing.T) {
	cases := map[string]string{
		"jobq.log":          "ok line\n\x00\x00\x00\x00tail\n",
		"scripts/run":       "#!/bin/sh\necho \x1b[31mred\n",
		"notes/plain-latin": "caf\xe9 and \x07 bell\n",
	}
	for rel, body := range cases {
		if got := runNonPrintableCheck(t, rel, body); got.Outcome != engine.OutcomeWarn {
			t.Errorf("%s: outcome = %s, want WARN", rel, got.Outcome)
		}
	}
}

func TestNoNonPrintable_BinaryExtensionSkipped(t *testing.T) {
	body := "\x00\x01\x02binary content\x1b\n"
	got := runNonPrintableCheck(t, "logo.png", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf(".png should be skipped; got %s", got.Outcome)
	}
}
