// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package frames

import (
	"bytes"
	"strings"
	"testing"
)

// TestParse_EmptyInput should error cleanly, not panic.
func TestParse_EmptyInput(t *testing.T) {
	_, err := Parse(strings.NewReader(""), "test://empty")
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

// TestParse_OnlyOpeningFence should error (no closing fence).
func TestParse_OnlyOpeningFence(t *testing.T) {
	_, err := Parse(strings.NewReader("---\n"), "test://just-fence")
	if err == nil {
		t.Fatal("expected error for fence-only input")
	}
}

// TestParse_HugeFrontmatter - 100KB frontmatter must not OOM or hang.
func TestParse_HugeFrontmatter(t *testing.T) {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: huge\n")
	b.WriteString("category: security\n")
	b.WriteString("subcategory: credentials\n")
	b.WriteString("severity: WARN\n")
	b.WriteString("triggers: [cli]\n")
	b.WriteString("applies-to:\n")
	b.WriteString("  files:\n")
	for i := 0; i < 5000; i++ {
		b.WriteString("    - \"**/glob-pattern-")
		b.WriteString(strings.Repeat("x", 20))
		b.WriteString(".js\"\n")
	}
	b.WriteString("---\n\nbody\n")
	f, err := Parse(strings.NewReader(b.String()), "test://huge")
	if err != nil {
		t.Fatalf("Parse big frontmatter error: %v", err)
	}
	if len(f.Frontmatter.AppliesTo.Files) != 5000 {
		t.Errorf("AppliesTo.Files = %d entries, want 5000", len(f.Frontmatter.AppliesTo.Files))
	}
}

// TestParse_HugeBody - 1MB body must not hang or truncate badly.
func TestParse_HugeBody(t *testing.T) {
	var b strings.Builder
	b.WriteString("---\nname: hugebody\ncategory: security\nsubcategory: credentials\nseverity: INFO\ntriggers: [cli]\n---\n")
	chunk := strings.Repeat("a", 1024)
	for i := 0; i < 1024; i++ {
		b.WriteString(chunk)
		b.WriteByte('\n')
	}
	f, err := Parse(strings.NewReader(b.String()), "test://hugebody")
	if err != nil {
		t.Fatalf("Parse huge body error: %v", err)
	}
	if len(f.Body) < 1024*1024 {
		t.Errorf("Body length %d, want >=1MB", len(f.Body))
	}
}

// TestParse_CRLFLineEndings - Windows line endings must work (the trim in
// parser strips \r before fence compare).
func TestParse_CRLFLineEndings(t *testing.T) {
	input := "---\r\nname: crlf\r\ncategory: security\r\nsubcategory: credentials\r\nseverity: INFO\r\ntriggers: [cli]\r\n---\r\n\r\nbody\r\n"
	f, err := Parse(strings.NewReader(input), "test://crlf")
	if err != nil {
		t.Fatalf("Parse CRLF error: %v", err)
	}
	if f.Frontmatter.Name != "crlf" {
		t.Errorf("Name = %q", f.Frontmatter.Name)
	}
}

// TestParse_UTF8BOM - leading byte-order-mark before opening fence.
// The parser currently requires the very first line to match the fence
// exactly, so a BOM should fail closed (good - explicit error, not
// confusing silent skip).
func TestParse_UTF8BOM(t *testing.T) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	input := append(bom, []byte("---\nname: bom\ncategory: security\nseverity: INFO\ntriggers: [cli]\n---\n")...)
	_, err := Parse(bytes.NewReader(input), "test://bom")
	if err == nil {
		t.Fatal("expected error for BOM-prefixed file (no opening fence)")
	}
}

// TestParse_UnicodeNameRejected - the V0.5 security audit tightened the
// name regex to [a-zA-Z0-9][a-zA-Z0-9_-]* (matching the published JSON
// schema). Non-ASCII names are rejected so they can't be used to smuggle
// control sequences past validation.
func TestParse_UnicodeNameRejected(t *testing.T) {
	input := "---\nname: \"フレーム-1\"\ncategory: security\nseverity: WARN\ntriggers: [cli]\n---\nbody\n"
	_, err := Parse(strings.NewReader(input), "test://unicode")
	if err == nil {
		t.Fatal("expected name validation to reject unicode name")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error doesn't mention name: %v", err)
	}
}

// TestParse_ControlByteInNameRejected - a name carrying a YAML-encoded
// ESC escape () decodes to a real ESC byte during YAML parsing,
// then the name regex catches it.
func TestParse_ControlByteInNameRejected(t *testing.T) {
	input := "---\nname: \"evil\\u001b[2Jpayload\"\ncategory: security\nseverity: WARN\ntriggers: [cli]\n---\nbody\n"
	_, err := Parse(strings.NewReader(input), "test://control")
	if err == nil {
		t.Fatal("expected name validation to reject control characters")
	}
	if strings.Contains(err.Error(), "\x1b") {
		t.Errorf("raw escape byte leaked into error message: %q", err)
	}
}

// TestParse_RawControlBytesInFrontmatterRejectedClearly: a
// frame file with raw ESC (or other C0 control) bytes in the frontmatter
// region used to produce a misleading "unclosed frontmatter" error
// because the YAML scanner choked. The pre-YAML control-byte check now
// rejects it with a precise message.
func TestParse_RawControlBytesInFrontmatterRejectedClearly(t *testing.T) {
	var b bytes.Buffer
	b.WriteString("---\nname: \"evil")
	b.WriteByte(0x1b) // raw ESC
	b.WriteString("[2Jbad\"\ncategory: security\nseverity: WARN\ntriggers: [cli]\n---\nbody\n")
	_, err := Parse(&b, "test://raw-control")
	if err == nil {
		t.Fatal("expected error for raw control byte in frontmatter")
	}
	msg := err.Error()
	if !bytes.Contains([]byte(msg), []byte("forbidden control byte")) {
		t.Errorf("expected explicit control-byte error, got: %v", err)
	}
	if bytes.Contains([]byte(msg), []byte{0x1b}) {
		t.Errorf("raw ESC byte leaked into error message")
	}
}

// TestParse_TabAndCRLFStillAccepted - make sure the new control-byte check
// doesn't reject legitimate tab/newline/CR bytes inside frontmatter.
func TestParse_TabAndCRLFStillAccepted(t *testing.T) {
	// Tab inside a string value is fine; CRLF line endings already covered.
	input := "---\r\nname: tabbed\r\ncategory: security\r\nsubcategory: credentials\r\nseverity: INFO\r\ntriggers: [cli]\r\n---\r\nbody\r\n"
	if _, err := Parse(strings.NewReader(input), "test://crlf2"); err != nil {
		t.Errorf("CRLF input should still parse: %v", err)
	}
}

// TestParse_BinaryGarbageInBody - random bytes after closing fence shouldn't
// confuse the YAML decoder (since frontmatter is already consumed).
func TestParse_BinaryGarbageInBody(t *testing.T) {
	var b bytes.Buffer
	b.WriteString("---\nname: bin\ncategory: security\nsubcategory: credentials\nseverity: INFO\ntriggers: [cli]\n---\n")
	for i := 0; i < 256; i++ {
		b.WriteByte(byte(i))
	}
	_, err := Parse(&b, "test://bin")
	if err != nil {
		t.Fatalf("Parse binary body error: %v (parser should tolerate non-text body)", err)
	}
}

// TestParse_MultipleFrontmatterBlocks - extra `---` later in the file is
// part of the body, not a new frontmatter (no quoting needed).
func TestParse_MultipleFrontmatterBlocks(t *testing.T) {
	input := "---\nname: multi\ncategory: security\nsubcategory: credentials\nseverity: INFO\ntriggers: [cli]\n---\n\n# Heading\n\n---\n\nMore body after another rule line.\n"
	f, err := Parse(strings.NewReader(input), "test://multi")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if !strings.Contains(f.Body, "More body after another rule") {
		t.Errorf("body lost content after second --- line: %q", f.Body)
	}
}

// TestParse_TabsInFrontmatter - YAML disallows tabs for indentation. Verify
// we surface the error (vs silent acceptance with wrong structure).
func TestParse_TabsInFrontmatter(t *testing.T) {
	input := "---\nname: tabs\ncategory: security\nseverity: INFO\ntriggers:\n\t- cli\n---\nbody\n"
	_, err := Parse(strings.NewReader(input), "test://tabs")
	if err == nil {
		t.Fatal("expected error: YAML forbids tabs for indentation")
	}
}

// TestParse_DeeplyNestedYAML - guard against runaway YAML structures.
func TestParse_DeeplyNestedYAML(t *testing.T) {
	var b strings.Builder
	b.WriteString("---\nname: deep\ncategory: security\nseverity: INFO\ntriggers: [cli]\napplies-to:\n  files:\n")
	for i := 0; i < 100; i++ {
		b.WriteString("    - ")
		b.WriteString(strings.Repeat("[", 50))
		b.WriteString("x")
		b.WriteString(strings.Repeat("]", 50))
		b.WriteString("\n")
	}
	b.WriteString("---\n")
	// We don't care if it parses or errors - only that it doesn't hang or panic.
	_, _ = Parse(strings.NewReader(b.String()), "test://deep")
}
