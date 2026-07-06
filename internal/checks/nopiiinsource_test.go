// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func piiWrite(t *testing.T, root, rel, content string) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func piiCtx(root string, changed ...string) engine.CheckContext {
	return engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ChangedFiles: changed}
}

func TestNoPIIInSource_LuhnValidCardFires(t *testing.T) {
	root := t.TempDir()
	// 4485275742300001 is Luhn-valid Visa, not a known test number.
	piiWrite(t, root, "db/seed.sql", "INSERT INTO cards VALUES ('4485275742300001');\n")
	res := NoPIIInSource(piiCtx(root))
	if res.Outcome != engine.OutcomeWarn {
		t.Fatalf("want WARN, got %v (%s)", res.Outcome, res.Reason)
	}
	if len(res.Hits) != 1 || res.Hits[0].Label != "payment card number (Luhn-valid)" {
		t.Fatalf("unexpected hits: %+v", res.Hits)
	}
}

func TestNoPIIInSource_CardWithSeparatorsFires(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "notes.txt", "card: 4485 2757 4230 0001\n")
	res := NoPIIInSource(piiCtx(root))
	if res.Outcome != engine.OutcomeWarn {
		t.Fatalf("want WARN, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoPIIInSource_KnownTestCardsPass(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "checkout_test.js", "const card = '4242424242424242'; // stripe test\nconst mc = '5555555555554444';\n")
	res := NoPIIInSource(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoPIIInSource_NonLuhnDigitsPass(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "metrics.log", "trace 4485275742300002 elapsed 1719859200000001\n")
	res := NoPIIInSource(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoPIIInSource_ValidIBANFires(t *testing.T) {
	root := t.TempDir()
	// DE75512108001245126199 passes mod-97 at the DE length.
	piiWrite(t, root, "fixtures/accounts.csv", "name,iban\nAcme,DE75512108001245126199\n")
	res := NoPIIInSource(piiCtx(root))
	if res.Outcome != engine.OutcomeWarn {
		t.Fatalf("want WARN, got %v (%s)", res.Outcome, res.Reason)
	}
	if len(res.Hits) != 1 || res.Hits[0].Label != "IBAN (checksum-valid)" {
		t.Fatalf("unexpected hits: %+v", res.Hits)
	}
}

func TestNoPIIInSource_ExampleIBANAndBadChecksumPass(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "docs/banking.md", "Use DE89370400440532013000 in examples.\nBroken: DE00512108001245126199\n")
	res := NoPIIInSource(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoPIIInSource_SSNWithContextFires(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "seed/users.sql", "UPDATE users SET ssn = '536-90-4399' WHERE id = 7;\n")
	res := NoPIIInSource(piiCtx(root))
	if res.Outcome != engine.OutcomeWarn {
		t.Fatalf("want WARN, got %v (%s)", res.Outcome, res.Reason)
	}
	if len(res.Hits) != 1 || res.Hits[0].Label != "SSN with identifying context" {
		t.Fatalf("unexpected hits: %+v", res.Hits)
	}
}

func TestNoPIIInSource_SSNShapeWithoutContextPasses(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "data.txt", "part number 536-90-4399 in stock\n")
	res := NoPIIInSource(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoPIIInSource_InvalidSSNAreasPass(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "users.sql", "ssn = '000-12-3456'; ssn = '666-12-3456'; ssn = '912-12-3456'; ssn = '123-00-4567'; ssn = '123-45-0000'\n")
	res := NoPIIInSource(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoPIIInSource_RedactionNeverEchoesValue(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "db/seed.sql", "INSERT INTO cards VALUES ('4485275742300001');\n")
	res := NoPIIInSource(piiCtx(root))
	if res.Outcome != engine.OutcomeWarn {
		t.Fatalf("want WARN, got %v", res.Outcome)
	}
	for _, s := range []string{res.Reason, res.Fix} {
		if strings.Contains(s, "4485275742300001") {
			t.Fatalf("matched value leaked into output: %q", s)
		}
	}
}

func TestNoPIIInSource_DisableMarkers(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "a.sql", "-- appframes:disable-next-line security/no-pii-in-source\nINSERT INTO cards VALUES ('4485275742300001');\n")
	piiWrite(t, root, "b.sql", "-- appframes:disable security/no-pii-in-source\nINSERT INTO cards VALUES ('5313598741020006');\n")
	res := NoPIIInSource(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoPIIInSource_BinaryFileSkipped(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "blob.bin", "prefix\x00 4485275742300001\n")
	res := NoPIIInSource(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoPIIInSource_PreCommitEmptyChangedFilesPasses(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "seed.sql", "INSERT INTO cards VALUES ('4485275742300001');\n")
	res := NoPIIInSource(engine.CheckContext{Trigger: engine.TriggerPreCommit, ProjectRoot: root})
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}
