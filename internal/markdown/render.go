package markdown

import (
	"strconv"
	"strings"
)

// This file is the renderer half of the port: it turns the block/inline model
// into styled terminal lines exactly the way upstream's Markdown component
// (packages/tui/src/components/markdown.ts) does. It is the renderer the TUI
// uses, so its output is pinned by tui/testdata/md_render_golden.txt.
//
// Terminal primitives (word wrap, visible width, background fills, image-line
// detection and OSC 8 hyperlinks) stay in the tui package and are injected
// through Terminal, so this package has no terminal dependency and the two
// packages cannot drift.

// DefaultStyle is the base styling applied to markdown text.
type DefaultStyle struct {
	Color         func(text string) string
	BgColor       func(text string) string
	Bold          bool
	Italic        bool
	Strikethrough bool
	Underline     bool
}

// Terminal is the terminal surface the renderer needs. tui implements it with
// its own wrap, width, background and hyperlink helpers.
type Terminal interface {
	Wrap(text string, width int) []string
	VisibleWidth(text string) int
	// ApplyBackground fills line to width with the background produced by bg.
	ApplyBackground(line string, width int, bg func(text string) string) string
	IsImageLine(line string) bool
	Hyperlink(text string, url string) string
}

// Options configure the renderer.
type Options struct {
	// Style paints each markdown element; nil means NopStyle.
	Style Style
	// Default is the base text style applied under the element styles.
	Default *DefaultStyle
	// Terminal supplies the terminal primitives; nil means PlainTerminal.
	Terminal                   Terminal
	Hyperlinks                 bool
	PreserveOrderedListMarkers bool
	PreserveBackslashEscapes   bool
	LatexEnabled               bool
	Latex                      func(source string, display bool) (string, bool)
	// HighlightCode, when set, replaces the per-line code styling.
	HighlightCode func(code string, lang string) []string
	// CodeBlockIndent prefixes code block lines (default "  ").
	CodeBlockIndent string
}

// Renderer renders blocks with a fixed style and terminal. Create one per
// render pass so the default-style prefix is computed once.
type Renderer struct {
	style            Style
	opts             Options
	term             Terminal
	defaultPrefix    string
	hasDefaultPrefix bool
}

// NewRenderer resolves the style and terminal defaults and returns a renderer.
func NewRenderer(opts Options) *Renderer {
	style := opts.Style
	if style == nil {
		style = NopStyle{}
	}
	term := opts.Terminal
	if term == nil {
		term = PlainTerminal{}
	}
	return &Renderer{style: style, opts: opts, term: term}
}

// RenderBlock renders one block to unwrapped content lines. next is the
// following block (nil at the end or inside a list item); it drives the
// trailing blank line rules.
func (r *Renderer) RenderBlock(b *Block, width int, next *Block) []string {
	return r.block(b, width, next, nil)
}

func (r *Renderer) block(b *Block, width int, next *Block, styleContext *inlineStyleContext) []string {
	var lines []string

	switch b.Kind {
	case BlockHeading:
		level := b.Level
		headingPrefix := strings.Repeat("#", level) + " "

		var headingStyleFn func(string) string
		if level == 1 {
			headingStyleFn = func(text string) string {
				return r.style.Heading(level, r.style.Bold(r.style.Underline(text)))
			}
		} else {
			headingStyleFn = func(text string) string {
				return r.style.Heading(level, r.style.Bold(text))
			}
		}
		headingStyleContext := inlineStyleContext{
			applyText:   headingStyleFn,
			stylePrefix: r.stylePrefixOf(headingStyleFn),
		}
		headingText := r.renderInlineTokens(b.Inlines, &headingStyleContext)
		styledHeading := headingText
		if level >= 3 {
			styledHeading = headingStyleFn(headingPrefix) + headingText
		}
		lines = append(lines, styledHeading)
		if next != nil && next.Kind != BlockSpace {
			lines = append(lines, "")
		}

	case BlockParagraph:
		lines = append(lines, r.renderInlineTokens(b.Inlines, styleContext))
		if next != nil && next.Kind != BlockList && next.Kind != BlockSpace {
			lines = append(lines, "")
		}

	case BlockCode:
		indent := r.opts.CodeBlockIndent
		if indent == "" {
			indent = "  "
		}
		lines = append(lines, r.style.CodeBlockBorder("```"+b.Info))
		if r.opts.HighlightCode != nil {
			for _, hlLine := range r.opts.HighlightCode(b.Body, b.Info) {
				lines = append(lines, indent+hlLine)
			}
		} else {
			for codeLine := range strings.SplitSeq(b.Body, "\n") {
				lines = append(lines, indent+r.style.CodeBlock(codeLine))
			}
		}
		lines = append(lines, r.style.CodeBlockBorder("```"))
		if next != nil && next.Kind != BlockSpace {
			lines = append(lines, "")
		}

	case BlockLatex:
		rendered := b.Raw
		if !b.Pending && r.opts.LatexEnabled && r.opts.Latex != nil {
			if latex, ok := r.opts.Latex(b.Text, true); ok {
				rendered = latex
			} else {
				rendered = jsTrim(b.Raw)
			}
		} else {
			rendered = jsTrim(b.Raw)
		}
		for line := range strings.SplitSeq(rendered, "\n") {
			lines = append(lines, r.applyDefaultStyle(line))
		}
		if next != nil && next.Kind != BlockSpace {
			lines = append(lines, "")
		}

	case BlockList:
		lines = append(lines, r.renderList(b, 0, width, styleContext)...)

	case BlockTable:
		lines = append(lines, r.renderTable(b, width, next, styleContext)...)

	case BlockQuote:
		quoteStyle := func(text string) string { return r.style.Quote(r.style.Italic(text)) }
		quoteStylePrefix := r.stylePrefixOf(quoteStyle)
		applyQuoteStyle := func(line string) string {
			if quoteStylePrefix == "" {
				return quoteStyle(line)
			}
			lineWithReappliedStyle := strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+quoteStylePrefix)
			return quoteStyle(lineWithReappliedStyle)
		}

		quoteContentWidth := max(1, width-2)
		quoteInlineStyleContext := inlineStyleContext{
			applyText:   func(text string) string { return text },
			stylePrefix: quoteStylePrefix,
		}

		var renderedQuoteLines []string
		quoteBlocks := b.Blocks
		for index, quoteBlock := range quoteBlocks {
			var nextQuote *Block
			if index+1 < len(quoteBlocks) {
				nextQuote = quoteBlocks[index+1]
			}
			renderedQuoteLines = append(renderedQuoteLines,
				r.block(quoteBlock, quoteContentWidth, nextQuote, &quoteInlineStyleContext)...)
		}

		for len(renderedQuoteLines) > 0 && renderedQuoteLines[len(renderedQuoteLines)-1] == "" {
			renderedQuoteLines = renderedQuoteLines[:len(renderedQuoteLines)-1]
		}

		for _, quoteLine := range renderedQuoteLines {
			styledLine := applyQuoteStyle(quoteLine)
			for _, wrappedLine := range r.term.Wrap(styledLine, quoteContentWidth) {
				lines = append(lines, r.style.QuoteBorder("│ ")+wrappedLine)
			}
		}
		if next != nil && next.Kind != BlockSpace {
			lines = append(lines, "")
		}

	case BlockThematicBreak:
		hrWidth := min(width, 80)
		lines = append(lines, r.style.ThematicBreak(strings.Repeat("─", max(0, hrWidth))))
		if next != nil && next.Kind != BlockSpace {
			lines = append(lines, "")
		}

	case BlockHTML:
		lines = append(lines, r.applyDefaultStyle(jsTrim(b.Raw)))

	case BlockSpace:
		lines = append(lines, "")

	default:
		if b.Text != "" {
			lines = append(lines, b.Text)
		}
	}

	return lines
}

// Finalize wraps one block's rendered lines, applies the horizontal margins and
// pads or background-fills each line to width.
func (r *Renderer) Finalize(rendered []string, width int, contentWidth int, paddingX int) []string {
	leftMargin := repeatSpacesM(max(0, paddingX))
	rightMargin := leftMargin
	var bgFn func(text string) string
	if r.opts.Default != nil {
		bgFn = r.opts.Default.BgColor
	}

	out := make([]string, 0, len(rendered))
	for _, line := range rendered {
		if r.term.IsImageLine(line) {
			out = append(out, line)
			continue
		}
		for _, wrapped := range r.term.Wrap(line, contentWidth) {
			lineWithMargins := leftMargin + wrapped + rightMargin
			if bgFn != nil {
				out = append(out, r.term.ApplyBackground(lineWithMargins, width, bgFn))
			} else {
				paddingNeeded := max(0, width-r.term.VisibleWidth(lineWithMargins))
				out = append(out, lineWithMargins+repeatSpacesM(paddingNeeded))
			}
		}
	}
	return out
}

type inlineStyleContext struct {
	applyText   func(text string) string
	stylePrefix string
}

func (r *Renderer) applyDefaultStyle(text string) string {
	if r.opts.Default == nil {
		return text
	}
	styled := text
	if r.opts.Default.Color != nil {
		styled = r.opts.Default.Color(styled)
	}
	if r.opts.Default.Bold {
		styled = r.style.Bold(styled)
	}
	if r.opts.Default.Italic {
		styled = r.style.Italic(styled)
	}
	if r.opts.Default.Strikethrough {
		styled = r.style.Strikethrough(styled)
	}
	if r.opts.Default.Underline {
		styled = r.style.Underline(styled)
	}
	return styled
}

func (r *Renderer) defaultStylePrefixValue() string {
	if r.opts.Default == nil {
		return ""
	}
	if r.hasDefaultPrefix {
		return r.defaultPrefix
	}

	const sentinel = "\u0000"
	styled := sentinel
	if r.opts.Default.Color != nil {
		styled = r.opts.Default.Color(styled)
	}
	if r.opts.Default.Bold {
		styled = r.style.Bold(styled)
	}
	if r.opts.Default.Italic {
		styled = r.style.Italic(styled)
	}
	if r.opts.Default.Strikethrough {
		styled = r.style.Strikethrough(styled)
	}
	if r.opts.Default.Underline {
		styled = r.style.Underline(styled)
	}

	sentinelIndex := strings.Index(styled, sentinel)
	if sentinelIndex >= 0 {
		r.defaultPrefix = styled[:sentinelIndex]
	} else {
		r.defaultPrefix = ""
	}
	r.hasDefaultPrefix = true
	return r.defaultPrefix
}

func (r *Renderer) stylePrefixOf(styleFn func(string) string) string {
	const sentinel = "\u0000"
	styled := styleFn(sentinel)
	before, _, ok := strings.Cut(styled, sentinel)
	if ok {
		return before
	}
	return ""
}

func (r *Renderer) defaultInlineStyleContext() inlineStyleContext {
	return inlineStyleContext{
		applyText:   func(text string) string { return r.applyDefaultStyle(text) },
		stylePrefix: r.defaultStylePrefixValue(),
	}
}

func (r *Renderer) renderInlineTokens(inlines []Inline, styleContext *inlineStyleContext) string {
	// A strings.Builder, not `result += ...`: this loop's iteration count scales
	// with the text, so appending to a string copied the whole accumulation each
	// time and made rendering one large message quadratic.
	var builder strings.Builder
	resolved := styleContext
	if resolved == nil {
		defaultContext := r.defaultInlineStyleContext()
		resolved = &defaultContext
	}
	applyText := resolved.applyText
	stylePrefix := resolved.stylePrefix

	applyTextWithNewlines := func(text string) string {
		segments := strings.Split(text, "\n")
		for index, segment := range segments {
			segments[index] = applyText(segment)
		}
		return strings.Join(segments, "\n")
	}

	for _, in := range inlines {
		switch in.Kind {
		case InlineLatex:
			rendered := in.Raw
			if !in.Pending && r.opts.LatexEnabled && r.opts.Latex != nil {
				if latex, ok := r.opts.Latex(in.Text, false); ok {
					rendered = latex
				}
			}
			builder.WriteString(applyTextWithNewlines(rendered))

		case InlineEscape:
			if r.opts.PreserveBackslashEscapes {
				builder.WriteString(applyTextWithNewlines(in.Raw))
			} else {
				builder.WriteString(applyTextWithNewlines(in.Text))
			}

		case InlineText:
			builder.WriteString(applyTextWithNewlines(in.Text))

		case InlineStrong:
			boldContent := r.renderInlineTokens(in.Children, resolved)
			builder.WriteString(r.style.Bold(boldContent) + stylePrefix)

		case InlineEmphasis:
			italicContent := r.renderInlineTokens(in.Children, resolved)
			builder.WriteString(r.style.Italic(italicContent) + stylePrefix)

		case InlineStrikethrough:
			delContent := r.renderInlineTokens(in.Children, resolved)
			builder.WriteString(r.style.Strikethrough(delContent) + stylePrefix)

		case InlineCode:
			builder.WriteString(r.style.Code(in.Text) + stylePrefix)

		case InlineLink:
			linkText := r.renderInlineTokens(in.Children, resolved)
			styledLink := r.style.Link(r.style.Underline(linkText))
			if r.opts.Hyperlinks {
				builder.WriteString(r.term.Hyperlink(styledLink, in.Dest) + stylePrefix)
			} else {
				hrefForComparison := in.Dest
				if strings.HasPrefix(hrefForComparison, "mailto:") {
					hrefForComparison = hrefForComparison[len("mailto:"):]
				}
				if in.Text == in.Dest || in.Text == hrefForComparison {
					builder.WriteString(styledLink + stylePrefix)
				} else {
					builder.WriteString(styledLink + r.style.LinkURL(" ("+in.Dest+")") + stylePrefix)
				}
			}

		case InlineImage:
			if in.Text != "" {
				builder.WriteString(applyTextWithNewlines(in.Text))
			}

		case InlineHardBreak:
			builder.WriteString("\n")

		case InlineHTML:
			builder.WriteString(applyTextWithNewlines(in.Text))

		default:
			if in.Text != "" {
				builder.WriteString(applyTextWithNewlines(in.Text))
			}
		}
	}

	result := builder.String()
	for stylePrefix != "" && strings.HasSuffix(result, stylePrefix) {
		result = result[:len(result)-len(stylePrefix)]
	}
	return result
}

func orderedListMarker(item *Block) (string, bool) {
	if item.Marker == "" {
		return "", false
	}
	return item.Marker + " ", true
}

func unorderedListMarker(item *Block) (string, bool) {
	if item.Marker == "" {
		return "", false
	}
	return item.Marker + " ", true
}

// renderList renders a list with nesting support.
func (r *Renderer) renderList(list *Block, depth int, width int, styleContext *inlineStyleContext) []string {
	var lines []string
	indent := strings.Repeat("    ", depth)
	startNumber := 1
	if list.StartNumber > 0 {
		startNumber = list.StartNumber
	}

	for index, item := range list.Blocks {
		isLastItem := index == len(list.Blocks)-1
		bullet := "- "
		if list.Ordered {
			if r.opts.PreserveOrderedListMarkers {
				if marker, ok := orderedListMarker(item); ok {
					bullet = marker
				} else {
					bullet = strconv.Itoa(startNumber+index) + ". "
				}
			} else {
				bullet = strconv.Itoa(startNumber+index) + ". "
			}
		} else if r.opts.PreserveOrderedListMarkers {
			if marker, ok := unorderedListMarker(item); ok {
				bullet = marker
			}
		}
		taskMarker := ""
		if item.Task {
			if item.Checked {
				taskMarker = "[x] "
			} else {
				taskMarker = "[ ] "
			}
		}
		marker := bullet + taskMarker
		firstPrefix := indent + r.style.Bullet(marker)
		continuationPrefix := indent + repeatSpacesM(r.term.VisibleWidth(marker))
		itemWidth := max(1, width-r.term.VisibleWidth(firstPrefix))
		renderedAnyLine := false

		for _, itemBlock := range item.Blocks {
			if itemBlock.Kind == BlockList {
				lines = append(lines, r.renderList(itemBlock, depth+1, width, styleContext)...)
				renderedAnyLine = true
				continue
			}
			itemLines := r.block(itemBlock, itemWidth, nil, styleContext)
			for _, line := range itemLines {
				for _, wrappedLine := range r.term.Wrap(line, itemWidth) {
					linePrefix := continuationPrefix
					if !renderedAnyLine {
						linePrefix = firstPrefix
					}
					lines = append(lines, linePrefix+wrappedLine)
					renderedAnyLine = true
				}
			}
		}

		if !renderedAnyLine {
			lines = append(lines, firstPrefix)
		}

		if !list.Tight && !isLastItem {
			lines = append(lines, "")
		}
	}

	return lines
}

// getLongestWordWidth returns the visible width of the longest word.
func (r *Renderer) getLongestWordWidth(text string, maxWidth int, hasMaxWidth bool) int {
	words := strings.Fields(text)
	longest := 0
	for _, word := range words {
		longest = max(longest, r.term.VisibleWidth(word))
	}
	if !hasMaxWidth {
		return longest
	}
	return min(longest, maxWidth)
}

// wrapCellText wraps a table cell, resetting styles after each non-final
// fragment and restoring the surrounding style.
func (r *Renderer) wrapCellText(text string, maxWidth int, stylePrefix string) []string {
	lines := r.term.Wrap(text, max(1, maxWidth))
	out := make([]string, 0, len(lines))
	for index, line := range lines {
		styleReset := ""
		if index < len(lines)-1 {
			styleReset = "\x1b[22;23;24;25;27;28;29;39m"
		}
		out = append(out, line+styleReset+stylePrefix)
	}
	return out
}

// renderTable renders a width-aware table.
func (r *Renderer) renderTable(table *Block, availableWidth int, next *Block, styleContext *inlineStyleContext) []string {
	var lines []string
	numCols := len(table.Header)
	if numCols == 0 {
		return lines
	}

	borderOverhead := 3*numCols + 1
	availableForCells := availableWidth - borderOverhead
	if availableForCells < numCols {
		// Too narrow for a stable table: fall back to the raw markdown.
		var fallbackLines []string
		if table.Raw != "" {
			fallbackLines = r.term.Wrap(table.Raw, availableWidth)
		}
		if next != nil && next.Kind != BlockSpace {
			fallbackLines = append(fallbackLines, "")
		}
		return fallbackLines
	}

	const maxUnbrokenWordWidth = 30

	naturalWidths := make([]int, numCols)
	minWordWidths := make([]int, numCols)
	for i := range numCols {
		headerText := r.renderInlineTokens(table.headerInlinesAt(i), styleContext)
		naturalWidths[i] = r.term.VisibleWidth(headerText)
		minWordWidths[i] = max(1, r.getLongestWordWidth(headerText, maxUnbrokenWordWidth, true))
	}
	for _, row := range table.rowInlines {
		for i := range row {
			cellText := r.renderInlineTokens(table.rowInlinesAt(row, i), styleContext)
			if i < numCols {
				naturalWidths[i] = max(naturalWidths[i], r.term.VisibleWidth(cellText))
				minWordWidths[i] = max(minWordWidths[i], r.getLongestWordWidth(cellText, maxUnbrokenWordWidth, true))
			}
		}
	}

	minColumnWidths := append([]int(nil), minWordWidths...)
	minCellsWidth := sumInts(minColumnWidths)

	if minCellsWidth > availableForCells {
		minColumnWidths = make([]int, numCols)
		for i := range minColumnWidths {
			minColumnWidths[i] = 1
		}
		remaining := availableForCells - numCols

		if remaining > 0 {
			totalWeight := 0
			for _, width := range minWordWidths {
				totalWeight += max(0, width-1)
			}
			growth := make([]int, numCols)
			for i, width := range minWordWidths {
				weight := max(0, width-1)
				if totalWeight > 0 {
					growth[i] = weight * remaining / totalWeight
				}
			}
			for i := range numCols {
				minColumnWidths[i] += growth[i]
			}
			allocated := sumInts(growth)
			leftover := remaining - allocated
			for i := 0; leftover > 0 && i < numCols; i++ {
				minColumnWidths[i]++
				leftover--
			}
		}
		minCellsWidth = sumInts(minColumnWidths)
	}

	totalNaturalWidth := sumInts(naturalWidths) + borderOverhead
	var columnWidths []int

	if totalNaturalWidth <= availableWidth {
		columnWidths = make([]int, numCols)
		for i := range numCols {
			columnWidths[i] = max(naturalWidths[i], minColumnWidths[i])
		}
	} else {
		totalGrowPotential := 0
		for i := range numCols {
			totalGrowPotential += max(0, naturalWidths[i]-minColumnWidths[i])
		}
		extraWidth := max(0, availableForCells-minCellsWidth)
		columnWidths = make([]int, numCols)
		for i := range numCols {
			naturalWidth := naturalWidths[i]
			minWidthDelta := max(0, naturalWidth-minColumnWidths[i])
			grow := 0
			if totalGrowPotential > 0 {
				grow = minWidthDelta * extraWidth / totalGrowPotential
			}
			columnWidths[i] = minColumnWidths[i] + grow
		}

		allocated := sumInts(columnWidths)
		remaining := availableForCells - allocated
		for remaining > 0 {
			grew := false
			for i := 0; i < numCols && remaining > 0; i++ {
				if columnWidths[i] < naturalWidths[i] {
					columnWidths[i]++
					remaining--
					grew = true
				}
			}
			if !grew {
				break
			}
		}
	}

	topBorderCells := make([]string, numCols)
	for i, w := range columnWidths {
		topBorderCells[i] = strings.Repeat("─", max(0, w))
	}
	lines = append(lines, "┌─"+strings.Join(topBorderCells, "─┬─")+"─┐")

	headerCellLines := make([][]string, numCols)
	for i := range numCols {
		text := r.renderInlineTokens(table.headerInlinesAt(i), styleContext)
		headerCellLines[i] = r.wrapCellText(text, columnWidths[i], stylePrefixOf(styleContext))
	}
	headerLineCount := maxLineCount(headerCellLines)

	for lineIdx := range headerLineCount {
		rowParts := make([]string, numCols)
		for colIdx := range numCols {
			text := ""
			if lineIdx < len(headerCellLines[colIdx]) {
				text = headerCellLines[colIdx][lineIdx]
			}
			padded := text + repeatSpacesM(max(0, columnWidths[colIdx]-r.term.VisibleWidth(text)))
			rowParts[colIdx] = r.style.Bold(padded)
		}
		lines = append(lines, "│ "+strings.Join(rowParts, " │ ")+" │")
	}

	separatorCells := make([]string, numCols)
	for i, w := range columnWidths {
		separatorCells[i] = strings.Repeat("─", max(0, w))
	}
	separatorLine := "├─" + strings.Join(separatorCells, "─┼─") + "─┤"
	lines = append(lines, separatorLine)

	for rowIndex, row := range table.rowInlines {
		rowCellLines := make([][]string, numCols)
		for i := range numCols {
			text := ""
			if i < len(row) {
				text = r.renderInlineTokens(table.rowInlinesAt(row, i), styleContext)
			}
			rowCellLines[i] = r.wrapCellText(text, columnWidths[i], stylePrefixOf(styleContext))
		}
		rowLineCount := maxLineCount(rowCellLines)

		for lineIdx := range rowLineCount {
			rowParts := make([]string, numCols)
			for colIdx := range numCols {
				text := ""
				if lineIdx < len(rowCellLines[colIdx]) {
					text = rowCellLines[colIdx][lineIdx]
				}
				rowParts[colIdx] = text + repeatSpacesM(max(0, columnWidths[colIdx]-r.term.VisibleWidth(text)))
			}
			lines = append(lines, "│ "+strings.Join(rowParts, " │ ")+" │")
		}

		if rowIndex < len(table.rowInlines)-1 {
			lines = append(lines, separatorLine)
		}
	}

	bottomBorderCells := make([]string, numCols)
	for i, w := range columnWidths {
		bottomBorderCells[i] = strings.Repeat("─", max(0, w))
	}
	lines = append(lines, "└─"+strings.Join(bottomBorderCells, "─┴─")+"─┘")

	if next != nil && next.Kind != BlockSpace {
		lines = append(lines, "")
	}
	return lines
}

// headerInlinesAt returns the header cell's inlines, empty when out of range.
func (b *Block) headerInlinesAt(i int) []Inline {
	if i < len(b.headerInlines) {
		return b.headerInlines[i]
	}
	return nil
}

// rowInlinesAt returns one row cell's inlines, empty when out of range.
func (b *Block) rowInlinesAt(row [][]Inline, i int) []Inline {
	if i < len(row) {
		return row[i]
	}
	return nil
}

func stylePrefixOf(styleContext *inlineStyleContext) string {
	if styleContext == nil {
		return ""
	}
	return styleContext.stylePrefix
}

func sumInts(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func maxLineCount(lines [][]string) int {
	maxCount := 0
	for _, entry := range lines {
		if len(entry) > maxCount {
			maxCount = len(entry)
		}
	}
	return maxCount
}

func repeatSpacesM(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.Repeat(" ", count)
}

// jsTrim is String.prototype.trim over the JS whitespace set.
func jsTrim(s string) string { return strings.TrimFunc(s, isWhitespaceRune) }

func isWhitespaceRune(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}
