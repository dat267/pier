package tui

import (
	"strings"
	"unicode/utf8"
)

// SanitizeUntrustedText removes terminal control sequences and control
// characters from text that a model, provider, or other remote peer supplied.
//
// The renderer builds its own styling (SGR), hyperlinks (OSC 8), and image
// escapes, so untrusted text has no legitimate use for any escape sequence.
// Letting one through reaches the terminal after the renderer's own filtering:
// a provider error or assistant message containing ESC[2J clears the screen,
// and cursor/OSC/DCS controls can do worse. Upstream passes them through; pier
// treats remote text as data (D342).
//
// Newlines and tabs survive because they are ordinary text layout; every other
// C0/C1 control is dropped.
func SanitizeUntrustedText(text string) string {
	if !hasTerminalControl(text) {
		return text
	}
	var result strings.Builder
	result.Grow(len(text))
	for i := 0; i < len(text); {
		char := text[i]
		switch {
		case char == '\x1b':
			i = skipEscapeSequence(text, i)
		case char == '\n' || char == '\t':
			result.WriteByte(char)
			i++
		case char < 0x20 || char == 0x7f:
			i++
		case char < utf8.RuneSelf:
			result.WriteByte(char)
			i++
		default:
			r, size := utf8.DecodeRuneInString(text[i:])
			if r == utf8.RuneError && size == 1 {
				// Invalid UTF-8: keep the byte, as the renderer does.
				result.WriteByte(char)
				i++
				continue
			}
			if r >= 0x80 && r <= 0x9f {
				i += size
				continue
			}
			result.WriteString(text[i : i+size])
			i += size
		}
	}
	return result.String()
}

// hasTerminalControl reports whether text contains anything SanitizeUntrustedText
// would remove, so ordinary text stays allocation-free.
func hasTerminalControl(text string) bool {
	for i := 0; i < len(text); i++ {
		char := text[i]
		if char == '\x1b' || char == 0x7f || (char < 0x20 && char != '\n' && char != '\t') {
			return true
		}
		if char == 0xc2 && i+1 < len(text) && text[i+1] >= 0x80 && text[i+1] <= 0x9f {
			return true
		}
	}
	return false
}

// skipEscapeSequence returns the index just past the escape sequence starting
// at text[start] (ESC). Malformed or unterminated sequences consume the rest of
// the text: dropping an unterminated OSC is safer than emitting its payload.
func skipEscapeSequence(text string, start int) int {
	next := start + 1
	if next >= len(text) {
		return len(text)
	}
	switch text[next] {
	case '[':
		return skipCSI(text, next+1)
	case ']':
		return skipControlString(text, next+1)
	case 'P', 'X', '^', '_':
		return skipControlString(text, next+1)
	}
	if text[next] >= 0x20 && text[next] <= 0x2f {
		// ESC with intermediates, e.g. ESC ( B (charset selection).
		position := next
		for position < len(text) && text[position] >= 0x20 && text[position] <= 0x2f {
			position++
		}
		if position < len(text) {
			position++
		}
		return position
	}
	return next + 1
}

// skipCSI returns the index just past a CSI sequence whose parameter bytes
// start at position.
func skipCSI(text string, position int) int {
	for position < len(text) && text[position] >= 0x30 && text[position] <= 0x3f {
		position++
	}
	for position < len(text) && text[position] >= 0x20 && text[position] <= 0x2f {
		position++
	}
	if position < len(text) && text[position] >= 0x40 && text[position] <= 0x7e {
		position++
	}
	return position
}

// skipControlString returns the index just past an OSC/DCS/SOS/PM/APC payload,
// which ends with BEL or the ST string terminator (ESC \).
func skipControlString(text string, position int) int {
	for position < len(text) {
		if text[position] == 0x07 {
			return position + 1
		}
		if text[position] == 0x1b && position+1 < len(text) && text[position+1] == '\\' {
			return position + 2
		}
		position++
	}
	return position
}
