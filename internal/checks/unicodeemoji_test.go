// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import "testing"

// A subdivision flag is the only legitimate use of tag runes. Everything else
// that looks flag-ish - a run with no terminator, a terminator with nothing to
// terminate - stays reportable, because those are not how a flag is encoded.
func TestTagSequenceLen(t *testing.T) {
	const flag = "\U0001F3F4"
	const cancel = "\U000E007F"
	tags := func(s string) string {
		out := ""
		for _, c := range s {
			out += string(rune(0xE0000 + c))
		}
		return out
	}
	scotland := flag + tags("gbsct") + cancel

	if n := tagSequenceLen(scotland); n != len(scotland) {
		t.Errorf("scotland flag: got %d want %d", n, len(scotland))
	}
	if n := tagSequenceLen(scotland + " trailing text"); n != len(scotland) {
		t.Errorf("flag followed by text: got %d want %d", n, len(scotland))
	}
	for name, s := range map[string]string{
		"unterminated":      flag + tags("gbsct"),
		"no letters":        flag + cancel,
		"bare tag rune":     tags("a") + cancel,
		"plain text":        "hello",
		"empty":             "",
		"flag alone":        flag,
		"letter in the run": flag + tags("gb") + "x" + tags("sct") + cancel,
	} {
		if n := tagSequenceLen(s); n != 0 {
			t.Errorf("%s: got %d, want 0 (not a well-formed flag)", name, n)
		}
	}
}

// A ZWJ composes one glyph from two emoji; between letters it is the identifier
// forgery the frame exists for. Either side touching a letter must stay a hit.
func TestZWJJoinsEmoji(t *testing.T) {
	const zwj = "‍"
	woman, laptop, tone := "\U0001F469", "\U0001F4BB", "\U0001F3FB"
	vs16 := "️"

	joins := map[string]string{
		"emoji both sides": woman + zwj + laptop,
		"skin tone before": woman + tone + zwj + laptop,
		"variation before": "❤" + vs16 + zwj + laptop,
		"variation after":  woman + zwj + "❤" + vs16,
	}
	for name, line := range joins {
		at := indexOfZWJ(line)
		if !zwjJoinsEmoji(line, at, len(zwj)) {
			t.Errorf("%s: want legitimate, got reportable", name)
		}
	}

	forgery := map[string]string{
		"letters both sides": "ad" + zwj + "min",
		"emoji then letter":  woman + zwj + "admin",
		"letter then emoji":  "admin" + zwj + laptop,
		"trailing joiner":    woman + zwj,
		"leading joiner":     zwj + laptop,
	}
	for name, line := range forgery {
		at := indexOfZWJ(line)
		if zwjJoinsEmoji(line, at, len(zwj)) {
			t.Errorf("%s: want reportable, got legitimate", name)
		}
	}
}

func indexOfZWJ(s string) int {
	for i := 0; i+3 <= len(s); i++ {
		if s[i:i+3] == "‍" {
			return i
		}
	}
	return -1
}
