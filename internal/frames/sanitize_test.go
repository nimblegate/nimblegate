// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package frames

import (
	"strings"
	"testing"
)

func TestSanitizeForOutput_PassesThroughPrintableAscii(t *testing.T) {
	in := "hello-world-123_abc"
	if got := SanitizeForOutput(in); got != in {
		t.Errorf("got %q, want %q", got, in)
	}
}

func TestSanitizeForOutput_PassesThroughUnicodeLetters(t *testing.T) {
	in := "フレーム münich Москва"
	if got := SanitizeForOutput(in); got != in {
		t.Errorf("got %q, want %q (unicode letters must round-trip)", got, in)
	}
}

func TestSanitizeForOutput_StripsAnsiCSI(t *testing.T) {
	// ESC [ 2 J  (clear screen) and ESC [ H (home cursor)
	in := "evil\x1b[2J\x1b[Hpayload"
	got := SanitizeForOutput(in)
	if strings.Contains(got, "\x1b") {
		t.Errorf("escape byte leaked through: %q", got)
	}
	if !strings.Contains(got, "evil") || !strings.Contains(got, "payload") {
		t.Errorf("payload text lost: %q", got)
	}
}

func TestSanitizeForOutput_StripsBareControlBytes(t *testing.T) {
	in := "a\x00b\x07c\x7fd"
	got := SanitizeForOutput(in)
	for _, bad := range []byte{0x00, 0x07, 0x7f} {
		if strings.IndexByte(got, bad) >= 0 {
			t.Errorf("control byte %#x leaked: %q", bad, got)
		}
	}
}

func TestSanitizeForOutput_PreservesTabAndNewlineAsEscapes(t *testing.T) {
	in := "line1\nline2\twith-tab"
	got := SanitizeForOutput(in)
	if strings.ContainsRune(got, '\n') {
		t.Errorf("raw newline preserved (should be \\n literal): %q", got)
	}
	if strings.ContainsRune(got, '\t') {
		t.Errorf("raw tab preserved: %q", got)
	}
	if !strings.Contains(got, `\n`) || !strings.Contains(got, `\t`) {
		t.Errorf("expected literal \\n and \\t markers: %q", got)
	}
}

func TestSanitizeForOutput_StripsZeroWidthAndFormatChars(t *testing.T) {
	// ZWJ (U+200D) is Cf (format).
	in := "vis‍ible"
	got := SanitizeForOutput(in)
	if strings.ContainsRune(got, '‍') {
		t.Errorf("zero-width joiner leaked: %q", got)
	}
}

func TestSanitizeForOutput_InvalidUTF8Escaped(t *testing.T) {
	// 0xff is never valid UTF-8.
	in := "ok\xff\xfetail"
	got := SanitizeForOutput(in)
	if strings.ContainsRune(got, 0xff) {
		t.Errorf("invalid byte leaked: %q", got)
	}
	if !strings.Contains(got, `\xff`) {
		t.Errorf("expected \\xff escape; got: %q", got)
	}
}

func TestSanitizeForOutput_EmptyInput(t *testing.T) {
	if got := SanitizeForOutput(""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
