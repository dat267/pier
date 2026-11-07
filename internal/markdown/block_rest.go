package markdown

import (
	"strings"
	"unicode/utf8"
)

// interruptsParagraph reports a line that ends a paragraph (the block starts
// that may interrupt a paragraph in Marked/CommonMark).
func interruptsParagraph(line string) bool {
	if _, _, ok := atxHeading(line); ok {
		return true
	}
	if _, _, ok := fenceStart(line); ok {
		return true
	}
	if isThematicBreak(line) {
		return true
	}
	if isBlockquoteStart(line) {
		return true
	}
	if listMarkerInterrupts(line) {
		return true
	}
	// The html alternative: marked's paragraph pattern continues a line that
	// does not start `</?tag` for a type-6 tag name or one of
	// script/pre/style/textarea/comment, and only at the very line start (its
	// other alternatives carry ` {0,3}` but this one does not, so `  <br>` does
	// not interrupt either). quoteHTMLInterrupt (quote.go) is that test.
	if line != "" && line[0] == '<' && quoteHTMLInterrupt.MatchString(line) {
		return true
	}
	return false
}

// listMarkerInterrupts reports a line whose list marker may interrupt a
// paragraph. The reference's paragraph rule is
// ` {0,3}(?:[*+-]|1[.)])[ \t]+[^ \t\n]`, so only an ordered marker numbered 1
// counts, and the marker must be followed by a non-space character: `text\n2. x`
// and `text\n1.` stay one paragraph, `text\n1. x` does not.
func listMarkerInterrupts(line string) bool {
	_, ordered, ok := listItemStart(line)
	if !ok {
		return false
	}
	if ordered {
		// The reference's pattern is `1[.)]`: the literal digit one, so `0. x`
		// does not interrupt. orderedStart normalizes 0 to 1 for a list's start
		// number, so the digits are read here instead.
		markerStart, _ := skipIndent(line, 4)
		markerEnd := markerStart
		for markerEnd < len(line) && line[markerEnd] >= '0' && line[markerEnd] <= '9' {
			markerEnd++
		}
		if line[markerStart:markerEnd] != "1" {
			return false
		}
	}
	// contentIndent is a visual column, so the marker end is what bounds the
	// content. A marker followed only by spaces has no content to interrupt.
	markerEnd := markerEndIndex(line)
	return strings.Trim(line[min(markerEnd, len(line)):], " \t") != ""
}

func (l *blockLexer) lexParagraph(start, to int) (*Block, int) {
	src := l.src
	at := start
	count := 0
	noSetext := false
	for at < to {
		ln := nextLine(src, at)
		if isBlankLine(ln.text) {
			break
		}
		if count > 0 {
			// A setext underline turns the whole paragraph into a heading, but
			// only when the lines the heading would swallow satisfy the
			// reference's lheading lookahead (marked 18.0.11): a line starting
			// any list marker (not only 1.) or the paragraph's own first line
			// looking like a delimiter row invalidates the conversion. marked
			// then lexes the same lines with its paragraph rule, which no
			// setext underline interrupts (`|lheading` is removed from it), so
			// the paragraph goes on unless the line interrupts it otherwise.
			if !noSetext {
				if level, ok := setextLevel(ln.text); ok {
					if lheadingAccepts(src, start, at) {
						headingEnd := ln.next
						// Tokenizer.lheading trims the text (marked 18.0.11).
						text := jsTrim(src[start:at])
						return &Block{Kind: BlockHeading, Start: start, End: headingEnd, Level: level, Text: text}, headingEnd
					}
					noSetext = true
				}
			}
			// A table header row or another block interrupts the paragraph.
			if l.looksLikeTableStart(at) || interruptsParagraph(ln.text) || l.latexInterrupts(at, ln.text) {
				// marked tries lheading on the block before its paragraph
				// rule, and lheading's text runs on past an interrupting line
				// its own lookahead accepts (`a\nb\n---`, `a\n<div>x\n---`).
				if !noSetext && lheadingLookahead(ln.text) {
					if b, end, ok := l.lheadingPast(start, at, to); ok {
						return b, end
					}
				}
				break
			}
		}
		count++
		at = ln.next
	}
	// The paragraph text is the source slice (the source already carries the
	// line separators), so it allocates nothing.
	text := strings.TrimSuffix(src[start:at], "\n")
	return &Block{Kind: BlockParagraph, Start: start, End: at, Text: text}, at
}

// latexInterrupts reports a `$$` or `\[` line that ends a paragraph. The
// latexBlock extension's start clips the paragraph before such a line, but
// when the tokenizer then declines it, marked's lastParagraphClipped joins the
// next paragraph back with a newline (pi v1.5.0 markdown.ts, marked 18.0.11
// Lexer.blockTokens), so only an opener the tokenizer takes interrupts.
func (l *blockLexer) latexInterrupts(at int, line string) bool {
	if !isLatexBlockStart(line) {
		return false
	}
	_, _, ok := l.lexLatex(at)
	return ok
}

// lheadingPast reports marked's lheading match for the paragraph at start
// when it reaches past the interrupting line at: every line through the
// underline must pass the lheading lookahead.
func (l *blockLexer) lheadingPast(start, at, to int) (*Block, int, bool) {
	src := l.src
	if !lheadingAccepts(src, start, at) {
		return nil, 0, false
	}
	for at < to {
		ln := nextLine(src, at)
		if level, ok := setextLevel(ln.text); ok {
			text := jsTrim(src[start:at])
			return &Block{Kind: BlockHeading, Start: start, End: ln.next, Level: level, Text: text}, ln.next, true
		}
		if !lheadingLookahead(ln.text) {
			break
		}
		at = ln.next
	}
	return nil, 0, false
}

// lheadingAccepts mirrors the alternation marked's lheading (18.0.11) uses to
// veto the conversion (packages/tui/src/components/markdown.ts lexes with
// marked): its text is lines that start no list marker (any ordered number, not
// only 1.), no code block, fence, quote, heading or full-line html tag, and
// none of them is delimiter-row shaped.
func lheadingAccepts(src string, start, underlineAt int) bool {
	for at := start; at < underlineAt; {
		ln := nextLine(src, at)
		if !lheadingLookahead(ln.text) {
			return false
		}
		at = ln.next
	}
	return true
}

// lheadingLookahead reports a line the reference's lheading rule accepts as its
// text: `(?:.|\n(?!...))+?` with the alternation
// `any list marker | code | fences | quote | heading | html-open | delimiter`.
func lheadingLookahead(line string) bool {
	if isBlankLine(line) {
		return false
	}
	if lheadingListStart(line) || lheadingCodeStart(line) {
		return false
	}
	if _, _, ok := fenceStart(line); ok {
		return false
	}
	if isBlockquoteStart(line) || lheadingHeadingStart(line) || lheadingHTMLOpen(line) {
		return false
	}
	return !delimiterRowShape(line)
}

// lheadingListStart is the lookahead's list alternative
// ` {0,3}(?:[*+-]|\d{1,9}[.)]) `: any bullet or ordered number followed by a
// space (the trailing space is part of the pattern).
func lheadingListStart(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i >= len(line) {
		return false
	}
	switch c := line[i]; {
	case c == '-' || c == '*' || c == '+':
		i++
	case c >= '0' && c <= '9':
		j := i
		for j < len(line) && j-i < 9 && line[j] >= '0' && line[j] <= '9' {
			j++
		}
		if j >= len(line) || (line[j] != '.' && line[j] != ')') {
			return false
		}
		i = j + 1
	default:
		return false
	}
	return i < len(line) && line[i] == ' '
}

// lheadingCodeStart is the code alternative ` {4}| {0,3}\t`.
func lheadingCodeStart(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i < len(line) {
		switch {
		case line[i] == '\t' && i < 4 || (i == 3 && line[i] == '\t'):
			return true
		case i == 3 && line[i] == ' ':
			return true
		}
	}
	return false
}

// lheadingHeadingStart is the heading alternative ` {0,3}#{1,6}(?:\s|$)`.
func lheadingHeadingStart(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	hashes := 0
	for i < len(line) && line[i] == '#' && hashes < 6 {
		hashes++
		i++
	}
	if hashes == 0 {
		return false
	}
	return i >= len(line) || (line[i] == ' ' || line[i] == '\t')
}

// lheadingHTMLOpen is the html alternative of the same lookahead,
// ` {0,3}<[^\n>]+>\n` (marked 18.0.11): one tag that fills the line blocks the
// heading (the lookahead newline is the line end here), while `<x>y` or
// `<a href=x>y</a>` does not.
func lheadingHTMLOpen(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if len(line)-i < 3 || line[i] != '<' || line[len(line)-1] != '>' {
		return false
	}
	return !strings.Contains(line[i+1:len(line)-1], ">")
}

// delimiterRowShape is the lookahead's delimiter-row alternative
// ` {0,3}\|?(?:[:\- ]*\|)+[\:\- ]*(?:\n|$)`, which a leading pipe (or none),
// units of colons/dashes/spaces ending in a pipe, and any trailing
// colon/dash/space run compose into.
func delimiterRowShape(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i < len(line) && line[i] == '|' {
		i++
	}
	units := 0
	for {
		if tailColonDashSpace(line[i:]) {
			return units > 0 || (units == 0 && i < len(line))
		}
		j := i
		for j < len(line) && strings.IndexByte(":- ", line[j]) >= 0 {
			j++
		}
		if j >= len(line) || line[j] != '|' {
			return false
		}
		i = j + 1
		units++
	}
}

func tailColonDashSpace(s string) bool {
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(":- ", s[i]) < 0 {
			return false
		}
	}
	return true
}

// looksLikeTableStart reports the paragraph rule's table interrupt: the line at
// at followed by a row of gfmTable's align shape (rules.ts, marked 18.0.11).
// The paragraph pattern only matches the shape; Tokenizer.table may still
// reject the rows, and then they lex as other blocks (`a\n2. b\n---` is a
// paragraph, a list and a rule).
func (l *blockLexer) looksLikeTableStart(at int) bool {
	header := nextLine(l.src, at)
	delim := nextLine(l.src, header.next)
	if delim.next <= header.next {
		return false
	}
	return alignRowShape(delim.text)
}

// alignRowShape matches gfmTable's align row
// ` {0,3}((?:\| *)?:?-+:? *(?:\| *:?-+:? *)*(?:\| *)?)` up to the line end.
func alignRowShape(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	skipSpaces := func() {
		for i < len(line) && line[i] == ' ' {
			i++
		}
	}
	cell := func() bool {
		if i < len(line) && line[i] == ':' {
			i++
		}
		dashes := i
		for i < len(line) && line[i] == '-' {
			i++
		}
		if i == dashes {
			return false
		}
		if i < len(line) && line[i] == ':' {
			i++
		}
		skipSpaces()
		return true
	}
	if i < len(line) && line[i] == '|' {
		i++
		skipSpaces()
	}
	if !cell() {
		return false
	}
	for i < len(line) && line[i] == '|' {
		i++
		skipSpaces()
		if i == len(line) {
			return true
		}
		if !cell() {
			return false
		}
	}
	return i == len(line)
}

// tableStartRows reports whether a delimiter row may start a table: it needs a
// pipe or a colon (tableDelimiter in Tokenizer.table, marked 18.0.11), so
// `| x |\n-` is a setext heading and `x\n:-` is a table.
func tableStartRows(delimiter string) bool {
	return strings.ContainsAny(delimiter, ":|")
}

func setextLevel(line string) (int, bool) {
	i, width := skipIndent(line, 4)
	if width >= 4 || i >= len(line) {
		return 0, false
	}
	ch := line[i]
	if ch != '=' && ch != '-' {
		return 0, false
	}
	j := i
	for j < len(line) && line[j] == ch {
		j++
	}
	if strings.Trim(line[j:], " \t") != "" {
		return 0, false
	}
	if ch == '=' {
		return 1, true
	}
	// A `-` run is a thematic break if it is 3+ and the whole line is dashes;
	// Marked treats a 1-2 dash underline as setext. A 3+ dash line after a
	// paragraph is a setext h2 as well.
	return 2, true
}

func (l *blockLexer) lexHTML(start int) (*Block, int) {
	src := l.src
	if closer := htmlBlockCloser(nextLine(src, start).text); closer != "" {
		// Types 1-5 end at their own closing token, which takes the rest of
		// its line with it; an unclosed block runs to the end of the source.
		end := len(src)
		if index := closingTokenIndex(src, closer, start); index >= 0 {
			end = nextLine(src, index).next
		}
		return &Block{Kind: BlockHTML, Start: start, End: end, Raw: src[start:end]}, end
	}
	at := start
	for at < len(src) {
		ln := nextLine(src, at)
		// The html rule ends types 6 and 7 at `(?:\n[ \t]*)+\n|$` (marked
		// 18.0.11): a blank line ends the block only when a newline follows
		// it, so a whitespace-only last line stays in it, and
		// trimTrailingBlankLines keeps that one line.
		if isBlankLine(ln.text) && at > start && at+len(ln.text) < len(src) {
			break
		}
		at = ln.next
	}
	return &Block{Kind: BlockHTML, Start: start, End: at, Raw: src[start:at]}, at
}

// htmlBlockCloser returns the token that ends a CommonMark HTML block of types
// 1-5, or "" for types 6 and 7, which Marked ends at a blank line: a script,
// pre, style or textarea element, a comment, a processing instruction, a
// declaration and a CDATA section. The reference's html rule carries the `i`
// flag, so the element names match case-insensitively.
func htmlBlockCloser(line string) string {
	i, width := skipIndent(line, 4)
	if width >= 4 || i >= len(line) || line[i] != '<' {
		return ""
	}
	rest := line[i+1:]
	switch {
	case strings.HasPrefix(rest, "!--"):
		return "-->"
	case strings.HasPrefix(rest, "?"):
		return "?>"
	case hasPrefixFold(rest, "![CDATA["):
		return "]]>"
	case rest != "" && rest[0] == '!' && len(rest) > 1 && isASCIILetter(rest[1]):
		return ">"
	}
	for _, name := range []string{"script", "pre", "style", "textarea"} {
		if !hasPrefixFold(rest, name) || len(rest) == len(name) {
			continue
		}
		if c := rest[len(name)]; c == ' ' || c == '\t' || c == '>' {
			return "</" + name + ">"
		}
	}
	return ""
}

// closingTokenIndex finds token in src at or after from. The element closers
// need case-insensitive matching (</PRE> closes <pre>); the punctuation-only
// ones do not.
func closingTokenIndex(src, token string, from int) int {
	if !strings.ContainsAny(token, "abcdefghijklmnopqrstuvwxyz") {
		if index := strings.Index(src[from:], token); index >= 0 {
			return from + index
		}
		return -1
	}
	for i := from; i+len(token) <= len(src); i++ {
		if hasPrefixFold(src[i:], token) {
			return i
		}
	}
	return -1
}

// hasPrefixFold reports a case-insensitive ASCII prefix match.
func hasPrefixFold(value, prefix string) bool {
	if len(value) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		left, right := value[i], prefix[i]
		if 'A' <= left && left <= 'Z' {
			left += 'a' - 'A'
		}
		if 'A' <= right && right <= 'Z' {
			right += 'a' - 'A'
		}
		if left != right {
			return false
		}
	}
	return true
}

func (l *blockLexer) lexIndentedCode(start int) (*Block, int) {
	src := l.src
	at := start
	type codeLine struct {
		text string
		end  int
	}
	var lines []codeLine
	for at < len(src) {
		ln := nextLine(src, at)
		if !isBlankLine(ln.text) && indentWidth(ln.text) < 4 {
			break
		}
		lines = append(lines, codeLine{ln.text, ln.next})
		at = ln.next
	}
	// The reference runs the raw through `te` (marked 18.0.11): trailing
	// blankish pieces are cut from the raw when two or more of them follow the
	// last content line. The pieces are the lines plus the empty piece that a
	// final newline contributes, so `    code\n \n` (a whitespace-only line
	// plus the final newline) loses both, while a single final newline stays
	// in the text (`    indented\n` keeps `text: "indented\n"`).
	trailing := 0
	for trailing < len(lines) && isBlankLine(lines[len(lines)-1-trailing].text) {
		trailing++
	}
	synthetic := at > start && src[at-1] == '\n'
	pieces := trailing
	if synthetic {
		pieces++
	}
	kept := len(lines)
	end := at
	var body string
	if pieces >= 2 {
		kept -= trailing
		if kept > 0 {
			end = lines[kept-1].end - 1 // without the newline the cut follows
		} else {
			end = start
		}
		for _, line := range lines[:kept] {
			if len(body) > 0 {
				body += "\n"
			}
			body += stripIndent(line.text, 4)
		}
	} else {
		for _, line := range lines {
			if len(body) > 0 {
				body += "\n"
			}
			body += stripIndent(line.text, 4)
		}
		if synthetic {
			body += "\n"
		}
	}
	return &Block{Kind: BlockCode, Start: start, End: end, Body: body, Raw: src[start:end]}, end
}

func stripIndent(line string, width int) string {
	i, consumed := 0, 0
	for i < len(line) && consumed < width {
		switch line[i] {
		case ' ':
			consumed++
		case '\t':
			consumed += 4
		default:
			return line[i:]
		}
		i++
	}
	return line[i:]
}

// stripTaskMarker removes a leading `[ ] ` / `[x] ` task checkbox the way
// marked's list tokenizer does (marked 18.0.11): listIsTask
// `^\[[ xX]\] +\S` marks the item, and listReplaceTask `^\[[ xX]\] +`
// strips it.
func stripTaskMarker(content string) (checked bool, rest string, ok bool) {
	// The tokenizer has expanded the first line's tabs to spaces by then
	// (listReplaceTabs), so a tab separates the box as a space does.
	if len(content) < 4 || content[0] != '[' || content[2] != ']' || (content[3] != ' ' && content[3] != '\t') {
		return false, content, false
	}
	mark := content[1]
	if mark != ' ' && mark != 'x' && mark != 'X' {
		return false, content, false
	}
	after := 3
	for after < len(content) && (content[after] == ' ' || content[after] == '\t') {
		after++
	}
	if after == len(content) {
		return false, content, false
	}
	if r, _ := utf8.DecodeRuneInString(content[after:]); isWhitespaceRune(r) {
		return false, content, false
	}
	return mark != ' ', content[after:], true
}

// markerEndIndex returns the index just past a list marker and its following
// spaces.
func markerEndIndex(line string) int {
	i, _ := skipIndent(line, 4)
	c := line[i]
	if c == '-' || c == '+' || c == '*' {
		i++
	} else {
		for i < len(line) && line[i] >= '0' && line[i] <= '9' {
			i++
		}
		i++ // . or )
	}
	spaces := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		if line[i] == '\t' {
			spaces += 4
		} else {
			spaces++
		}
		i++
		if spaces >= 5 {
			break
		}
	}
	return i
}

func orderedStart(line string) int {
	i, _ := skipIndent(line, 4)
	n := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		n = n*10 + int(line[i]-'0')
		i++
	}
	if n == 0 {
		n = 1
	}
	return n
}

func lexBlocksIn(src string) []*Block {
	// A list item or quote chunk usually holds one or two blocks; a top-level
	// document's 16-slot start is wasted allocation here.
	return (&blockLexer{src: src}).lexBlocksCap(0, len(src), 2)
}

// isHTMLBlockStart reports a line that starts marked's html block rule
// (marked 18.0.11, block `html`, which carries the `i` flag): a script, pre,
// style or textarea element, a comment, a processing instruction, a
// declaration or CDATA (types 1-5), an opening or closing tag from the block
// tag list followed by spaces, the line end, `>` or `/>` (type 6), or a
// complete open or closing tag of any other name alone on its line (type 7).
// An inline tag followed by text on its line (`<span>x</span>`, `<kbd>a</kbd>
// b`) is not a block start: the line is paragraph text with inline HTML.
func isHTMLBlockStart(line string) bool {
	i, width := skipIndent(line, 4)
	if width >= 4 || i+1 >= len(line) || line[i] != '<' {
		return false
	}
	rest := line[i:]
	switch {
	case strings.HasPrefix(rest, "<!--"), strings.HasPrefix(rest, "<?"), strings.HasPrefix(rest, "<![CDATA["):
		return true
	case rest[1] == '!':
		return len(rest) > 2 && isASCIILetter(rest[2])
	}
	closing := rest[1] == '/'
	nameStart := 1
	if closing {
		nameStart = 2
	}
	j := nameStart
	for j < len(rest) && (isAlnum(rest[j]) || rest[j] == '-' || rest[j] == '_') {
		j++
	}
	if j == nameStart || !isASCIILetter(rest[nameStart]) {
		return false
	}
	name := strings.ToLower(rest[nameStart:j])
	after := rest[j:]
	// Type 1: `<(script|pre|style|textarea)[\s>]`, opening tags only.
	if !closing && (name == "script" || name == "pre" || name == "style" || name == "textarea") {
		return after == "" || after[0] == '>' || after[0] == ' ' || after[0] == '\t'
	}
	// Type 6: a block tag name, then ` +`, the line end, `>` or `/>`. The
	// tag alternation is tried prefix-first, so the name may be a prefix of
	// the run (`<h1x>` is not a block tag, but `<header>` is).
	for k := len(name); k > 0; k-- {
		if !markedBlockTag(name[:k]) {
			continue
		}
		tail := rest[nameStart+k:]
		if tail == "" || tail[0] == ' ' || tail[0] == '>' || strings.HasPrefix(tail, "/>") {
			return true
		}
	}
	// Type 7 excludes names starting with script, pre, style or textarea
	// (`(?!script|pre|style|textarea)`).
	for _, excluded := range []string{"script", "pre", "style", "textarea"} {
		if strings.HasPrefix(name, excluded) {
			return false
		}
	}
	if closing {
		// `</name\s*>` and nothing but spaces or tabs after it.
		k := strings.TrimLeft(after, " \t")
		return strings.HasPrefix(k, ">") && strings.TrimLeft(k[1:], " \t") == ""
	}
	end, ok := htmlOpenTagAttributesEnd(after)
	return ok && strings.TrimLeft(after[end:], " \t") == ""
}

// htmlOpenTagAttributesEnd matches marked's `(?:attribute)*? */?>` after an
// open tag's name and returns the offset after the `>`. An attribute is
// ` +[a-zA-Z:_][\w.:-]*` with an optional ` *= *` value: double-quoted,
// single-quoted (neither spanning a line) or unquoted `[^\s"'=<>\x60]+`.
func htmlOpenTagAttributesEnd(s string) (int, bool) {
	i := 0
	for {
		j := i
		for j < len(s) && s[j] == ' ' {
			j++
		}
		if j < len(s) && s[j] == '>' {
			return j + 1, true
		}
		if j+1 < len(s) && s[j] == '/' && s[j+1] == '>' {
			return j + 2, true
		}
		// Another attribute needs at least one space before its name.
		if j == i || j >= len(s) || !(isASCIILetter(s[j]) || s[j] == ':' || s[j] == '_') {
			return 0, false
		}
		j++
		for j < len(s) && (isAlnum(s[j]) || s[j] == '_' || s[j] == '.' || s[j] == ':' || s[j] == '-') {
			j++
		}
		k := j
		for k < len(s) && s[k] == ' ' {
			k++
		}
		if k < len(s) && s[k] == '=' {
			k++
			for k < len(s) && s[k] == ' ' {
				k++
			}
			switch {
			case k < len(s) && (s[k] == '"' || s[k] == '\''):
				close := strings.IndexByte(s[k+1:], s[k])
				if close < 0 {
					return 0, false
				}
				j = k + 1 + close + 1
			default:
				v := k
				for v < len(s) && !strings.ContainsRune(" \t\"'=<>\x60", rune(s[v])) {
					v++
				}
				if v == k {
					return 0, false
				}
				j = v
			}
		}
		i = j
	}
}

// markedBlockTag reports a name in marked's type-6 block tag list.
func markedBlockTag(name string) bool {
	_, ok := markedBlockTagSet[name]
	return ok
}

var markedBlockTagSet = func() map[string]struct{} {
	set := map[string]struct{}{}
	for _, tag := range strings.Split(markedBlockTags, "|") {
		if tag == "h[1-6]" {
			for _, n := range "123456" {
				set["h"+string(n)] = struct{}{}
			}
			continue
		}
		set[tag] = struct{}{}
	}
	return set
}()

// startsHTMLLine is the broad tag-at-line-start check a table body ends at.
func startsHTMLLine(line string) bool {
	i, width := skipIndent(line, 4)
	if width >= 4 || i >= len(line) || line[i] != '<' {
		return false
	}
	if i+1 >= len(line) {
		return false
	}
	c := line[i+1]
	if c == '!' || c == '?' {
		return true
	}
	j := i + 1
	if c == '/' {
		j++
	}
	if j >= len(line) || !isASCIILetter(line[j]) {
		return false
	}
	for j < len(line) && (isAlnum(line[j]) || line[j] == '-') {
		j++
	}
	if j >= len(line) {
		return true
	}
	switch line[j] {
	case ' ', '\t', '>', '/':
		return true
	}
	return false
}

// tryTable detects a GFM table: a header row with a `|` followed by a
// delimiter row of dashes and colons.
func (l *blockLexer) tryTable(start int) (*Block, bool) {
	src := l.src
	header := nextLine(src, start)
	delim := nextLine(src, header.next)
	if delim.next <= header.next {
		return nil, false
	}
	// gfmTable's align row (at most three spaces of indent, spaces only) and
	// Tokenizer.table's pipe-or-colon check (marked 18.0.11).
	if !alignRowShape(delim.text) || !tableStartRows(delim.text) {
		return nil, false
	}
	aligns, ok := parseDelimiterRow(delim.text)
	if !ok {
		return nil, false
	}
	// Tokenizer.table rejects a header whose cell count differs from the
	// delimiter row's (marked 18.0.11): `x y\n:-: | -` is a paragraph.
	headers := splitTableRow(header.text)
	if len(headers) != len(aligns) {
		return nil, false
	}
	table := &Block{Kind: BlockTable, Start: start, Align: aligns}
	table.Header = headers
	at := delim.next
	for at < len(src) {
		ln := nextLine(src, at)
		// A table body ends at a blank line or at another block; any other line
		// is a row, even without a pipe, and its cells are fitted to the header
		// count (Marked's table rule; verified against marked 18.0.11).
		if isBlankLine(ln.text) || startsOtherBlock(ln.text) {
			break
		}
		table.Rows = append(table.Rows, tableRowCells(ln.text, len(aligns)))
		at = ln.next
	}
	table.End = at
	table.Raw = src[start:at]
	return table, true
}

// startsOtherBlock reports whether a line starts a block that ends a table
// body: a heading, fence, thematic break, blockquote, list, HTML block or
// indented code.
func startsOtherBlock(line string) bool {
	if _, _, ok := atxHeading(line); ok {
		return true
	}
	if _, _, ok := fenceStart(line); ok {
		return true
	}
	if isThematicBreak(line) || isBlockquoteStart(line) || startsHTMLLine(line) || indentWidth(line) >= 4 {
		return true
	}
	_, _, ok := listItemStart(line)
	return ok
}

// tableRowCells splits a table row and fits it to the table's column count the
// way Marked does: extra cells are dropped and missing ones are empty.
func tableRowCells(line string, columns int) []string {
	cells := splitTableRow(line)
	if len(cells) > columns {
		return cells[:columns]
	}
	for len(cells) < columns {
		cells = append(cells, "")
	}
	return cells
}

func parseDelimiterRow(line string) ([]Align, bool) {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	if len(parts) == 0 {
		return nil, false
	}
	aligns := make([]Align, 0, len(parts))
	for _, part := range parts {
		cell := strings.TrimSpace(part)
		if cell == "" {
			return nil, false
		}
		left := strings.HasPrefix(cell, ":")
		right := strings.HasSuffix(cell, ":")
		mid := strings.Trim(cell, ":")
		if mid == "" {
			return nil, false
		}
		for i := 0; i < len(mid); i++ {
			if mid[i] != '-' {
				return nil, false
			}
		}
		switch {
		case left && right:
			aligns = append(aligns, AlignCenter)
		case left:
			aligns = append(aligns, AlignLeft)
		case right:
			aligns = append(aligns, AlignRight)
		default:
			aligns = append(aligns, AlignNone)
		}
	}
	return aligns, true
}

// splitTableRow is marked's splitCells (helpers.ts, marked 18.0.11) without a
// count: an unescaped pipe delimits cells, an empty first or last cell is
// dropped, and each cell is trimmed with its `\|` unescaped.
func splitTableRow(line string) []string {
	var cells []string
	cellStart := 0
	for i := 0; i < len(line); i++ {
		if line[i] != '|' {
			continue
		}
		slashes := 0
		for j := i - 1; j >= 0 && line[j] == '\\'; j-- {
			slashes++
		}
		if slashes%2 == 0 {
			cells = append(cells, line[cellStart:i])
			cellStart = i + 1
		}
	}
	cells = append(cells, line[cellStart:])
	if jsTrim(cells[0]) == "" {
		cells = cells[1:]
	}
	if len(cells) > 0 && jsTrim(cells[len(cells)-1]) == "" {
		cells = cells[:len(cells)-1]
	}
	for i, cell := range cells {
		cells[i] = strings.ReplaceAll(jsTrim(cell), `\|`, "|")
	}
	return cells
}
