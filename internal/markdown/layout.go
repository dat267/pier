package markdown

import (
	"strings"
	"unicode/utf8"
)

// This file is the layout layer: the standalone RenderSource/Render entry
// points, the default PlainTerminal, and the width/wrap primitives. The block
// and inline painting lives in render.go, over the Style interface; there is
// one renderer, not a separate layout tree.

// RenderSource lexes src and renders it to styled lines at the given width,
// without padding: each line is wrapped but not padded to the full width. The
// component wrapper applies margins and padding.
func RenderSource(src string, width int, style Style) []string {
	return Render(Lex(src), width, style)
}

// Render lays out blocks as styled lines no wider than width.
func Render(blocks []*Block, width int, style Style) []string {
	if width < 1 {
		width = 1
	}
	renderer := NewRenderer(Options{Style: style, Terminal: PlainTerminal{}, LatexEnabled: true})
	var lines []string
	for i, b := range blocks {
		var next *Block
		if i+1 < len(blocks) {
			next = blocks[i+1]
		}
		for _, line := range renderer.RenderBlock(b, width, next) {
			lines = append(lines, renderer.term.Wrap(line, width)...)
		}
	}
	return lines
}

// PlainTerminal is the default Terminal: the package's own wrap, width,
// background and OSC 8 helpers, so the standalone renderer needs no terminal
// library. The TUI injects its own terminal primitives instead.
type PlainTerminal struct{}

func (PlainTerminal) Wrap(text string, width int) []string { return wrapStyled(text, width) }

func (PlainTerminal) VisibleWidth(text string) int { return visibleWidth(text) }

func (PlainTerminal) ApplyBackground(line string, width int, bg func(text string) string) string {
	return bg(line + repeatSpacesM(max(0, width-visibleWidth(line))))
}

func (PlainTerminal) IsImageLine(string) bool { return false }

func (PlainTerminal) Hyperlink(text string, url string) string {
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// wrapStyled wraps a styled string to width. Each word is a self-contained
// styled run (its active SGR codes are re-applied and reset within the run), so
// a break never leaves an unbalanced escape.
func wrapStyled(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for line := range strings.SplitSeq(s, "\n") {
		out = append(out, wrapStyledLine(line, width)...)
	}
	return out
}

type styledToken struct {
	s     string
	w     int
	space bool
}

// tokenizeStyled splits a styled line into words and single spaces, making each
// word a self-contained styled run.
func tokenizeStyled(line string) []styledToken {
	var tokens []styledToken
	active := ""
	wordPrefix := ""
	inWord := false
	var cur strings.Builder

	flush := func() {
		if cur.Len() == 0 {
			return
		}
		body := cur.String()
		text := wordPrefix + body
		// Close the run only when it is still open. A style that the source
		// already reset inside the word would otherwise get a second, redundant
		// reset and make the escape count odd.
		if active != "" {
			text += "\x1b[0m"
		}
		tokens = append(tokens, styledToken{s: text, w: visibleWidth(body)})
		cur.Reset()
		inWord = false
		wordPrefix = ""
	}

	for i := 0; i < len(line); {
		if line[i] == '\x1b' {
			code, n := extractANSI(line[i:])
			active = mergeSGR(active, code)
			if inWord {
				cur.WriteString(code)
			}
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if r == ' ' {
			flush()
			tokens = append(tokens, styledToken{s: " ", w: 1, space: true})
			i += size
			continue
		}
		if !inWord {
			inWord = true
			wordPrefix = active
		}
		cur.WriteRune(r)
		i += size
	}
	flush()
	return tokens
}

func wrapStyledLine(line string, width int) []string {
	if strings.IndexByte(line, 0x1b) < 0 {
		return wrapPlain(line, width)
	}
	if visibleWidth(line) <= width {
		return []string{line}
	}
	tokens := tokenizeStyled(line)
	var out []string
	var cur strings.Builder
	col := 0
	for _, tok := range tokens {
		if !tok.space && col > 0 && col+tok.w > width {
			out = append(out, strings.TrimRight(cur.String(), " "))
			cur.Reset()
			col = 0
		}
		if tok.space && col == 0 {
			continue
		}
		cur.WriteString(tok.s)
		col += tok.w
	}
	out = append(out, strings.TrimRight(cur.String(), " "))
	return out
}

// wrapPlain wraps an escape-free line, returning substrings of the input so the
// common case allocates only the result slice.
func wrapPlain(line string, width int) []string {
	var out []string
	lineStart := 0
	col := 0
	lastSpace := -1
	colAtSpace := 0
	i := 0
	for i < len(line) {
		r, size := utf8.DecodeRuneInString(line[i:])
		w := runeWidth(r)
		if r == ' ' {
			if col+1 > width && lineStart < i {
				out = append(out, line[lineStart:i])
				lineStart = i + size
				col = 0
				lastSpace = -1
				i += size
				continue
			}
			lastSpace = i
			colAtSpace = col
			col++
			i += size
			continue
		}
		if col+w > width {
			if lastSpace >= lineStart {
				out = append(out, line[lineStart:lastSpace])
				lineStart = lastSpace + 1
				col -= colAtSpace + 1
				lastSpace = -1
			} else if lineStart < i {
				out = append(out, line[lineStart:i])
				lineStart = i
				col = 0
			}
		}
		col += w
		i += size
	}
	out = append(out, line[lineStart:])
	return out
}

// extractANSI returns the ANSI escape sequence at the start of s and its length.
func extractANSI(s string) (string, int) {
	if len(s) < 2 || s[0] != '\x1b' {
		return s[:1], 1
	}
	switch s[1] {
	case '[':
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return s[:i+1], i + 1
			}
		}
		return s, len(s)
	case ']':
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return s[:i+1], i + 1
			}
			if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\' {
				return s[:i+2], i + 2
			}
		}
		return s, len(s)
	default:
		return s[:2], 2
	}
}

// mergeSGR updates the active SGR prefix with a new escape sequence.
func mergeSGR(active, code string) string {
	if !strings.HasPrefix(code, "\x1b[") || !strings.HasSuffix(code, "m") {
		return active
	}
	body := code[2 : len(code)-1]
	if body == "" || body == "0" {
		return ""
	}
	return active + code
}

// visibleWidth is the display width of a styled line.
func visibleWidth(s string) int {
	width := 0
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			_, n := extractANSI(s[i:])
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		width += runeWidth(r)
		i += size
	}
	return width
}

// runeWidth is a compact East Asian width: zero-width for combining marks and
// control bytes, double for the CJK/emoji ranges, otherwise one.
func runeWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r < 0x20:
		return 0
	case r >= 0x0300 && r <= 0x036f:
		return 0
	case r >= 0x1ab0 && r <= 0x1aff:
		return 0
	case r >= 0x20d0 && r <= 0x20ff:
		return 0
	case r >= 0xfe00 && r <= 0xfe0f:
		return 0
	case r >= 0x1100 && r <= 0x115f,
		r >= 0x2e80 && r <= 0x303e,
		r >= 0x3041 && r <= 0x33ff,
		r >= 0x3400 && r <= 0x4dbf,
		r >= 0x4e00 && r <= 0x9fff,
		r >= 0xa000 && r <= 0xa4cf,
		r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe30 && r <= 0xfe4f,
		r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f300 && r <= 0x1f64f,
		r >= 0x1f900 && r <= 0x1f9ff,
		r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}
