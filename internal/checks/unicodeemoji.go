// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import "unicode/utf8"

// Emoji legitimately use two of the rune classes the invisible-payload frames
// look for, and at the byte level a legitimate use is indistinguishable from an
// attack unless the surrounding structure is read:
//
//   - a subdivision flag (🏴󠁧󠁢󠁳󠁣󠁴󠁿) is U+1F3F4 followed by tag letters and
//     terminated by U+E007F. That is the standard encoding, so a README naming
//     Scotland or Wales was rejected at BLOCK severity.
//   - a ZWJ sequence (👩‍💻) joins two emoji with U+200D. Any UI string
//     carrying a profession or family emoji was rejected the same way.
//
// The attack shapes are unaffected: a tag rune outside a flag sequence has no
// legitimate use, and the identifier forgery this frame exists for is a joiner
// between two letters, not between two emoji.

// waveWhiteFlag opens every subdivision flag sequence.
const waveBlackFlag = 0x1F3F4

// tagSequenceLen returns the byte length of a well-formed subdivision flag
// sequence at the start of s, or 0 when s does not begin with one. A sequence
// is U+1F3F4, one or more tag letters (U+E0020..U+E007E), then the cancel tag
// U+E007F. Anything else - a bare tag rune, an unterminated run - returns 0 and
// is reported.
func tagSequenceLen(s string) int {
	r, size := utf8.DecodeRuneInString(s)
	if r != waveBlackFlag {
		return 0
	}
	i, letters := size, 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r >= 0xE0020 && r <= 0xE007E:
			letters++
			i += size
		case r == 0xE007F:
			if letters == 0 {
				return 0
			}
			return i + size
		default:
			return 0
		}
	}
	return 0
}

// isEmojiRune reports whether r is a pictographic rune that a ZWJ may join.
// Deliberately broad over the emoji blocks rather than exact: the question is
// "is this a picture, not a letter", and a letter is what the frame protects.
func isEmojiRune(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF: // emoji blocks incl. supplemental
		return true
	case r >= 0x2600 && r <= 0x27BF: // misc symbols + dingbats
		return true
	case r >= 0x2B00 && r <= 0x2BFF: // misc symbols and arrows
		return true
	case r == 0x00A9 || r == 0x00AE: // (c) (r), emoji-presentable
		return true
	}
	return false
}

// isEmojiModifier reports whether r only decorates an adjacent emoji: a
// variation selector or a skin-tone modifier. Skipped when looking either side
// of a joiner.
func isEmojiModifier(r rune) bool {
	return r == 0xFE0F || r == 0xFE0E || (r >= 0x1F3FB && r <= 0x1F3FF)
}

// zwjJoinsEmoji reports whether the ZWJ at byte offset zwjAt in line sits
// between two emoji - the legitimate use. before is the text preceding it.
func zwjJoinsEmoji(line string, zwjAt int, zwjSize int) bool {
	prev := lastMeaningfulRune(line[:zwjAt])
	if !isEmojiRune(prev) {
		return false
	}
	rest := line[zwjAt+zwjSize:]
	for len(rest) > 0 {
		r, size := utf8.DecodeRuneInString(rest)
		if isEmojiModifier(r) {
			rest = rest[size:]
			continue
		}
		return isEmojiRune(r)
	}
	return false
}

// lastMeaningfulRune returns the final rune of s, skipping emoji modifiers so
// a skin-toned or variation-selected emoji still reads as an emoji.
func lastMeaningfulRune(s string) rune {
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if isEmojiModifier(r) {
			s = s[:len(s)-size]
			continue
		}
		return r
	}
	return 0
}
