// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package frames

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// SanitizeForOutput strips ANSI escape sequences and other control
// characters from a frame-field value before it is echoed in error
// messages, lint output, or audit log Reason fields. Non-printable bytes
// are replaced with `\xNN`; valid printable Unicode (letters, digits,
// punctuation, common symbols, whitespace) is preserved.
//
// Use this anywhere a frame-supplied string is interpolated into
// user-facing output to defeat terminal-injection attacks via crafted
// frontmatter (e.g. a `name:` containing `\x1b[2J\x1b[H` that would
// otherwise clear the user's screen).
func SanitizeForOutput(s string) string {
	if s == "" {
		return s
	}
	if !utf8.ValidString(s) {
		// Replace invalid UTF-8 bytes with explicit escapes.
		var b strings.Builder
		for i := 0; i < len(s); {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				b.WriteString(escapeByte(s[i]))
				i++
				continue
			}
			b.WriteString(SanitizeForOutput(string(r)))
			i += size
		}
		return b.String()
	}

	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n':
			// Permit tab + newline - they don't reposition the cursor in a
			// way that hides text. Render tab as space + literal escape so
			// downstream UI can show it without ambiguity.
			if r == '\t' {
				b.WriteString(`\t`)
			} else {
				b.WriteString(`\n`)
			}
		case r < 0x20, r == 0x7f:
			// C0 control chars + DEL - strip via escape.
			b.WriteString(escapeRune(r))
		case unicode.Is(unicode.Cc, r), unicode.Is(unicode.Cf, r):
			// Unicode "Cc" = control, "Cf" = format (zero-width joiner, etc.).
			b.WriteString(escapeRune(r))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func escapeByte(b byte) string {
	const hex = "0123456789abcdef"
	return `\x` + string(hex[b>>4]) + string(hex[b&0x0f])
}

func escapeRune(r rune) string {
	if r < 0x80 {
		return escapeByte(byte(r))
	}
	// Render multi-byte controls as \uXXXX for clarity.
	const hex = "0123456789abcdef"
	return `\u` +
		string(hex[(r>>12)&0xf]) +
		string(hex[(r>>8)&0xf]) +
		string(hex[(r>>4)&0xf]) +
		string(hex[r&0xf])
}
