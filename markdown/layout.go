package markdown

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// RenderSource lexes src and renders it to styled lines at the given width.
func RenderSource(src string, width int, style Style) []string {
	blocks := Lex(src)
	return Render(blocks, width, style)
}

// Render lays out blocks as styled lines no wider than width.
func Render(blocks []*Block, width int, style Style) []string {
	if width < 1 {
		width = 1
	}
	r := &renderer{width: width, style: style, lines: make([]string, 0, 64)}
	r.renderBlocks(blocks, "")
	return r.lines
}

type renderer struct {
	width int
	style Style
	lines []string
}

func (r *renderer) emit(line string) { r.lines = append(r.lines, line) }

func (r *renderer) renderBlocks(blocks []*Block, prefix string) {
	for _, b := range blocks {
		r.renderBlock(b, prefix)
	}
}

// renderBlock renders one block with the given line prefix (a quote's `> ` or a
// list's hanging indent).
func (r *renderer) renderBlock(b *Block, prefix string) {
	width := r.width - visibleWidth(prefix)
	if width < 1 {
		width = 1
	}
	switch b.Kind {
	case BlockParagraph:
		r.emitWrapped(renderInline(b.Inlines, r.style), width, prefix)
	case BlockHeading:
		r.emitWrapped(r.style.Heading(b.Level, renderInline(b.Inlines, r.style)), width, prefix)
	case BlockCode:
		r.emit(prefix + r.style.CodeBlockBorder("```"+b.Info))
		for _, line := range strings.Split(b.Body, "\n") {
			r.emit(prefix + r.style.CodeBlock(line))
		}
		r.emit(prefix + r.style.CodeBlockBorder("```"))
	case BlockThematicBreak:
		r.emit(prefix + r.style.ThematicBreak(strings.Repeat("─", max(1, width))))
	case BlockQuote:
		r.renderBlocks(b.Blocks, prefix+r.style.QuoteBorder("> "))
	case BlockList:
		r.renderList(b, prefix)
	case BlockTable:
		r.renderTable(b, prefix)
	case BlockHTML:
		r.emitRawLines(b.Raw, prefix, r.style.HTML)
	case BlockLatex:
		r.emitRawLines(b.Raw, prefix, r.style.Plain)
	case BlockSpace:
		r.emit("")
	}
}

func (r *renderer) emitRawLines(raw, prefix string, style func(string) string) {
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		r.emit(prefix + style(line))
	}
}

func (r *renderer) renderList(list *Block, prefix string) {
	number := list.StartNumber
	if number == 0 {
		number = 1
	}
	for i, item := range list.Blocks {
		marker := "- "
		if list.Ordered {
			marker = strconv.Itoa(number+i) + ". "
		}
		if item.Task {
			if item.Checked {
				marker += "[x] "
			} else {
				marker += "[ ] "
			}
		}
		bullet := prefix + r.style.Bullet(marker)
		indent := prefix + strings.Repeat(" ", visibleWidth(marker))
		first := true
		for _, child := range item.Blocks {
			if child.Kind == BlockSpace {
				r.emit("")
				continue
			}
			childPrefix := indent
			if first {
				childPrefix = bullet
			}
			r.renderBlock(child, childPrefix)
			first = false
		}
	}
}

func (r *renderer) renderTable(table *Block, prefix string) {
	cols := len(table.Align)
	if len(table.Header) > cols {
		cols = len(table.Header)
	}
	if cols == 0 {
		return
	}
	widths := make([]int, cols)
	header := make([][]Inline, cols)
	for i := 0; i < cols; i++ {
		header[i] = inlineSliceAt(table.headerInlines, i)
		widths[i] = visibleWidth(renderInline(header[i], r.style))
	}
	rows := make([][][]Inline, len(table.Rows))
	for ri := range table.Rows {
		rows[ri] = make([][]Inline, cols)
		for ci := 0; ci < cols; ci++ {
			rows[ri][ci] = inlineSliceAt(table.rowInlines[ri], ci)
			if w := visibleWidth(renderInline(rows[ri][ci], r.style)); w > widths[ci] {
				widths[ci] = w
			}
		}
	}
	line := func(cells [][]Inline) string {
		var b strings.Builder
		b.WriteString(prefix)
		b.WriteString("|")
		for ci := 0; ci < cols; ci++ {
			text := renderInline(cells[ci], r.style)
			b.WriteString(" ")
			b.WriteString(padCell(text, widths[ci], alignAt(table.Align, ci)))
			b.WriteString(" |")
		}
		return b.String()
	}
	r.emit(line(header))
	separator := make([][]Inline, cols)
	for ci := 0; ci < cols; ci++ {
		separator[ci] = []Inline{{Kind: InlineText, Text: strings.Repeat("─", max(1, widths[ci]))}}
	}
	r.emit(line(separator))
	for _, row := range rows {
		r.emit(line(row))
	}
}

func inlineSliceAt(grid [][]Inline, i int) []Inline {
	if i < len(grid) {
		return grid[i]
	}
	return nil
}

func alignAt(aligns []Align, i int) Align {
	if i < len(aligns) {
		return aligns[i]
	}
	return AlignNone
}

func padCell(text string, width int, align Align) string {
	gap := width - visibleWidth(text)
	if gap < 0 {
		gap = 0
	}
	switch align {
	case AlignRight:
		return strings.Repeat(" ", gap) + text
	case AlignCenter:
		left := gap / 2
		return strings.Repeat(" ", left) + text + strings.Repeat(" ", gap-left)
	default:
		return text + strings.Repeat(" ", gap)
	}
}

func (r *renderer) emitWrapped(styled string, width int, prefix string) {
	for _, line := range wrapStyled(styled, width) {
		r.emit(prefix + line)
	}
}

// renderInline flattens inlines to a styled string.
func renderInline(inlines []Inline, style Style) string {
	var b strings.Builder
	renderInlineInto(&b, inlines, style)
	return b.String()
}

func renderInlineInto(b *strings.Builder, inlines []Inline, style Style) {
	for _, in := range inlines {
		switch in.Kind {
		case InlineText:
			b.WriteString(style.Plain(in.Text))
		case InlineCode:
			b.WriteString(style.Code(in.Text))
		case InlineStrong:
			b.WriteString(style.Bold(renderInline(in.Children, style)))
		case InlineEmphasis:
			b.WriteString(style.Italic(renderInline(in.Children, style)))
		case InlineStrikethrough:
			b.WriteString(style.Strikethrough(renderInline(in.Children, style)))
		case InlineLink:
			b.WriteString(style.Link(renderInline(in.Children, style)))
		case InlineImage:
			b.WriteString(style.Link(in.Text))
		case InlineHardBreak:
			b.WriteByte('\n')
		case InlineEscape, InlineLatex:
			b.WriteString(style.Plain(in.Text))
		case InlineHTML:
			b.WriteString(style.HTML(in.Text))
		case InlineCheckbox:
			// Rendered by the list marker.
		}
	}
}

// wrapStyled wraps a styled string to width. Each word is a self-contained
// styled run (its active SGR codes are re-applied and reset within the run), so
// a break never leaves an unbalanced escape.
func wrapStyled(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
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
		if wordPrefix != "" {
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
