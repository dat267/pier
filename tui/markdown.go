package tui

import (
	"strings"

	"github.com/dat267/pier/markdown"
)

// TerminalCapabilities holds the terminal feature flags the markdown renderer
// consults (upstream getCapabilities(); divergence D69: injectable instead of
// environment-detected).
type TerminalCapabilities struct {
	Hyperlinks bool
	TrueColor  bool
	// Images is the inline-image protocol ("" | "kitty" | "iterm2"); the
	// transport itself is out of scope (D57/D81).
	Images string
}

var terminalCapabilitiesState struct {
	capabilities TerminalCapabilities
}

// GetTerminalCapabilities returns the capability flags.
func GetTerminalCapabilities() TerminalCapabilities {
	return terminalCapabilitiesState.capabilities
}

// SetTerminalCapabilities installs the capability flags the markdown renderer and the
// alternate screen consult. The interactive mode installs them at boot; D69 keeps them
// injected rather than detected inside this package.
func SetTerminalCapabilities(capabilities TerminalCapabilities) {
	terminalCapabilitiesState.capabilities = capabilities
}

// DefaultTextStyle is the base styling applied to markdown text.
type DefaultTextStyle struct {
	Color         func(text string) string
	BgColor       func(text string) string
	Bold          bool
	Italic        bool
	Strikethrough bool
	Underline     bool
}

// MarkdownTheme styles markdown elements.
type MarkdownTheme struct {
	Heading         func(text string) string
	Link            func(text string) string
	LinkURL         func(text string) string
	Code            func(text string) string
	CodeBlock       func(text string) string
	CodeBlockBorder func(text string) string
	Quote           func(text string) string
	QuoteBorder     func(text string) string
	Hr              func(text string) string
	ListBullet      func(text string) string
	Bold            func(text string) string
	Italic          func(text string) string
	Strikethrough   func(text string) string
	Underline       func(text string) string
	// HighlightCode, when set, replaces the per-line code styling.
	HighlightCode func(code string, lang string) []string
	// CodeBlockIndent prefixes code block lines (default "  ").
	CodeBlockIndent string
}

// MarkdownOptions configure the Markdown component.
type MarkdownOptions struct {
	// PreserveOrderedListMarkers keeps source list markers instead of
	// normalizing them.
	PreserveOrderedListMarkers bool
	// PreserveBackslashEscapes keeps source backslash escapes.
	PreserveBackslashEscapes bool
	// Transform transforms the source markdown with the available content
	// width.
	Transform func(markdown string, availableWidth int) string
	// RenderLatex toggles LaTeX rendering (default true).
	RenderLatex *bool
}

// markdownTerminal adapts the tui's terminal primitives to the renderer's
// injection surface (markdown.Terminal), so the renderer logic lives in the
// markdown package without depending on tui.
type markdownTerminal struct{}

func identityStyle(text string) string { return text }

func (markdownTerminal) Wrap(text string, width int) []string {
	return WrapTextWithAnsi(text, width)
}
func (markdownTerminal) VisibleWidth(text string) int { return VisibleWidth(text) }
func (markdownTerminal) ApplyBackground(line string, width int, bg func(text string) string) string {
	return ApplyBackgroundToLine(line, width, bg)
}
func (markdownTerminal) IsImageLine(line string) bool { return IsImageLine(line) }
func (markdownTerminal) Hyperlink(text string, url string) string {
	return Hyperlink(text, url)
}

// Markdown renders markdown text for the terminal.
type Markdown struct {
	Text             string
	PaddingX         int
	PaddingY         int
	Theme            MarkdownTheme
	DefaultTextStyle *DefaultTextStyle
	Options          MarkdownOptions

	cachedText     string
	hasCachedText  bool
	cachedWidth    int
	hasCachedWidth bool
	cachedLines    []string
	hasCachedLines bool

	// Incremental render cache. Streaming appends to the source, so the block
	// list keeps a stable prefix; cacheTokenLines holds the final (wrapped,
	// padded) lines for the previous block list's prefix minus its last block,
	// so an append only re-renders the changed tail instead of the whole
	// message.
	//
	// The comparison key is held strongly (a small value per top-level block)
	// while the parsed block trees are not retained between renders: a block
	// tree is about ten times its source and every transcript message keeps a
	// Markdown component (upstream 54c19a252 holds its parse cache weakly for
	// the same reason). The key carries the fields that decide whether a block
	// changed, so the prefix reuse does not depend on GC timing; the strings it
	// references are substrings of the cached source text, which is retained
	// anyway.
	cacheTokenKeys  []mdBlockKey
	cacheTokenLines [][]string
	cacheWidth      int

	// renderedTokens counts block renders (test seam).
	renderedTokens int

	preparation           *MarkdownPreparation
	preparationGeneration uint64
	preparing             bool
	preparingText         string
	preparingWidth        int
	displayLines          []string
}

// NewMarkdown creates a markdown component.
func NewMarkdown(text string, paddingX int, paddingY int, theme MarkdownTheme, defaultTextStyle *DefaultTextStyle, options MarkdownOptions) *Markdown {
	return &Markdown{
		Text:             text,
		PaddingX:         paddingX,
		PaddingY:         paddingY,
		Theme:            theme,
		DefaultTextStyle: defaultTextStyle,
		Options:          options,
	}
}

// SetText updates the markdown source. The incremental cache is kept: Render
// reuses the prefix that is still identical and re-renders the changed tail.
func (m *Markdown) SetText(text string) {
	m.preparationGeneration++
	m.preparing = false
	m.Text = text
	m.cachedLines = nil
	m.hasCachedLines = false
	m.hasCachedText = false
	m.hasCachedWidth = false
}

// SetTextIfChanged is SetText that keeps everything when the text has not moved.
// Dropping the line cache costs a full re-lex on the next render, which
// dominates when a component is rebuilt for reasons other than its text (a
// visibility toggle rebuilding every message in a long transcript, for
// instance). Callers that also change the transform or the styles must use
// Invalidate instead: the same text can render differently.
func (m *Markdown) SetTextIfChanged(text string) {
	if m.Text == text {
		return
	}
	m.SetText(text)
}

// Invalidate drops the render cache.
func (m *Markdown) Invalidate() {
	m.preparationGeneration++
	m.preparing = false
	m.cachedLines = nil
	m.hasCachedLines = false
	m.hasCachedText = false
	m.hasCachedWidth = false
	m.cacheTokenKeys = nil
	m.cacheTokenLines = nil
	m.cacheWidth = 0
}

// Prepare warms the render cache for width without returning the lines, so an
// off-loop worker can pre-render a large message before the loop paints it.
// Render reads only the component's own captured theme, text and transform, so
// calling it here is safe as long as the component is not attached to the
// rendered tree (the loop must not Render the same instance concurrently).
func (m *Markdown) Prepare(width int) {
	preparation := m.preparation
	m.preparation = nil
	defer func() { m.preparation = preparation }()
	_ = m.Render(width)
}

// renderOptions captures the component's styling for one render pass.
func (m *Markdown) renderOptions() markdown.Options {
	return markdown.Options{
		Theme: markdown.Theme{
			Heading:         m.Theme.Heading,
			Link:            m.Theme.Link,
			LinkURL:         m.Theme.LinkURL,
			Code:            m.Theme.Code,
			CodeBlock:       m.Theme.CodeBlock,
			CodeBlockBorder: m.Theme.CodeBlockBorder,
			Quote:           m.Theme.Quote,
			QuoteBorder:     m.Theme.QuoteBorder,
			Hr:              m.Theme.Hr,
			ListBullet:      m.Theme.ListBullet,
			Bold:            m.Theme.Bold,
			Italic:          m.Theme.Italic,
			Strikethrough:   m.Theme.Strikethrough,
			Underline:       m.Theme.Underline,
			HighlightCode:   m.Theme.HighlightCode,
			CodeBlockIndent: m.Theme.CodeBlockIndent,
		},
		Default:                    markdownDefaultStyle(m.DefaultTextStyle),
		Terminal:                   markdownTerminal{},
		Hyperlinks:                 GetTerminalCapabilities().Hyperlinks,
		PreserveOrderedListMarkers: m.Options.PreserveOrderedListMarkers,
		PreserveBackslashEscapes:   m.Options.PreserveBackslashEscapes,
		LatexEnabled:               m.renderLatexEnabled(),
		Latex: func(source string, display bool) (string, bool) {
			return RenderLatex(source, RenderLatexOptions{Display: display})
		},
	}
}

func markdownDefaultStyle(style *DefaultTextStyle) *markdown.DefaultStyle {
	if style == nil {
		return nil
	}
	return &markdown.DefaultStyle{
		Color:         style.Color,
		BgColor:       style.BgColor,
		Bold:          style.Bold,
		Italic:        style.Italic,
		Strikethrough: style.Strikethrough,
		Underline:     style.Underline,
	}
}

// Render renders the markdown at the given width.
func (m *Markdown) Render(width int) []string {
	if m.preparing && m.preparingWidth != width {
		m.preparationGeneration++
		m.preparing = false
	}
	if m.hasCachedLines && m.hasCachedText && m.cachedText == m.Text && m.hasCachedWidth && m.cachedWidth == width {
		return m.cachedLines
	}

	if m.preparation != nil && len(m.Text) >= largeMarkdownBytes {
		return m.prepareLarge(width)
	}

	contentWidth := max(1, width-m.PaddingX*2)
	text := m.Text
	if m.Options.Transform != nil {
		text = m.Options.Transform(text, contentWidth)
	}

	if text == "" || jsTrim(text) == "" {
		m.cachedText = m.Text
		m.hasCachedText = true
		m.cachedWidth = width
		m.hasCachedWidth = true
		m.cachedLines = []string{}
		m.hasCachedLines = true
		m.displayLines = m.cachedLines
		m.cacheTokenKeys = nil
		m.cacheTokenLines = nil
		m.cacheWidth = width
		return m.cachedLines
	}

	normalizedText := strings.ReplaceAll(text, "\t", "   ")

	blocks := markdown.Lex(normalizedText)
	markdown.TrimPartialClosingFences(blocks)
	renderer := markdown.NewRenderer(m.renderOptions())

	// Reuse the final lines of every block that did not change since the last
	// render; only the changed tail is re-parsed and re-styled.
	reuse := m.reusableBlocks(blocks, m.cacheTokenKeys, width)

	var contentLines []string
	tokenLines := make([][]string, 0, max(0, len(blocks)-1))
	for index, block := range blocks {
		var final []string
		if index < reuse {
			final = m.cacheTokenLines[index]
		} else {
			var next *markdown.Block
			if index+1 < len(blocks) {
				next = blocks[index+1]
			}
			m.renderedTokens++
			rendered := renderer.RenderBlock(block, contentWidth, next)
			final = renderer.Finalize(rendered, width, contentWidth, m.PaddingX)
		}
		contentLines = append(contentLines, final...)
		if index < len(blocks)-1 {
			tokenLines = append(tokenLines, final)
		}
	}
	m.cacheTokenKeys = markdownBlockKeys(blocks)
	m.cacheTokenLines = tokenLines
	m.cacheWidth = width

	var bgFn func(text string) string
	if m.DefaultTextStyle != nil {
		bgFn = m.DefaultTextStyle.BgColor
	}
	emptyLine := repeatSpaces(width)
	var emptyLines []string
	for i := 0; i < m.PaddingY; i++ {
		line := emptyLine
		if bgFn != nil {
			line = ApplyBackgroundToLine(emptyLine, width, bgFn)
		}
		emptyLines = append(emptyLines, line)
	}

	result := make([]string, 0, len(emptyLines)*2+len(contentLines))
	result = append(result, emptyLines...)
	result = append(result, contentLines...)
	result = append(result, emptyLines...)

	m.cachedText = m.Text
	m.hasCachedText = true
	m.cachedWidth = width
	m.hasCachedWidth = true
	m.cachedLines = result
	m.hasCachedLines = true
	m.displayLines = result

	if len(result) > 0 {
		return result
	}
	return []string{""}
}

// reusableBlocks returns how many leading blocks of the current render can
// reuse the cached final lines. A block's lines were rendered with the next
// block's kind as context, so the last matching block is only reused when its
// successor also matches.
// D180: upstream 54c19a252 also flattens its cached lines (flattenLines): V8
// keeps string concatenations as ropes, and an unflattened cache retains a tree
// of the concatenated parts. Go strings are immutable flat buffers, so there is
// no equivalent representation to flatten.
func (m *Markdown) reusableBlocks(blocks []*markdown.Block, previous []mdBlockKey, width int) int {
	if m.cacheWidth != width || len(m.cacheTokenLines) == 0 {
		return 0
	}
	limit := len(blocks)
	if len(previous) < limit {
		limit = len(previous)
	}
	matched := 0
	for matched < limit && sameMdBlockKey(blocks[matched], previous[matched]) {
		matched++
	}
	reuse := matched - 1
	maxReuse := len(m.cacheTokenLines)
	if len(blocks)-1 < maxReuse {
		maxReuse = len(blocks) - 1
	}
	if maxReuse < 0 {
		maxReuse = 0
	}
	if reuse > maxReuse {
		reuse = maxReuse
	}
	if reuse < 0 {
		reuse = 0
	}
	return reuse
}

// mdBlockKey is the set of top-level fields that decide whether a block changed
// since the previous render (sameMdBlockKey), held without the block trees
// themselves.
type mdBlockKey struct {
	Kind        markdown.BlockKind
	Start       int
	End         int
	Level       int
	Text        string
	Raw         string
	Info        string
	Body        string
	Marker      string
	Ordered     bool
	StartNumber int
	Tight       bool
	Task        bool
	Checked     bool
	Pending     bool
	AlignLen    int
	BlocksLen   int
	InlinesLen  int
}

func markdownBlockKey(block *markdown.Block) mdBlockKey {
	return mdBlockKey{
		Kind: block.Kind, Start: block.Start, End: block.End, Level: block.Level,
		Text: block.Text, Raw: block.Raw, Info: block.Info, Body: block.Body,
		Marker: block.Marker, Ordered: block.Ordered, StartNumber: block.StartNumber,
		Tight: block.Tight, Task: block.Task, Checked: block.Checked, Pending: block.Pending,
		AlignLen: len(block.Align), BlocksLen: len(block.Blocks), InlinesLen: len(block.Inlines),
	}
}

func markdownBlockKeys(blocks []*markdown.Block) []mdBlockKey {
	keys := make([]mdBlockKey, len(blocks))
	for i, block := range blocks {
		keys[i] = markdownBlockKey(block)
	}
	return keys
}

func sameMdBlockKey(block *markdown.Block, key mdBlockKey) bool {
	if block == nil {
		return false
	}
	return block.Kind == key.Kind && block.Start == key.Start && block.End == key.End &&
		block.Level == key.Level && block.Text == key.Text && block.Raw == key.Raw &&
		block.Info == key.Info && block.Body == key.Body && block.Marker == key.Marker &&
		block.Ordered == key.Ordered && block.StartNumber == key.StartNumber &&
		block.Tight == key.Tight && block.Task == key.Task && block.Checked == key.Checked &&
		block.Pending == key.Pending && len(block.Align) == key.AlignLen &&
		len(block.Blocks) == key.BlocksLen && len(block.Inlines) == key.InlinesLen
}

func (m *Markdown) renderLatexEnabled() bool {
	if m.Options.RenderLatex == nil {
		return true
	}
	return *m.Options.RenderLatex
}

var _ Component = (*Markdown)(nil)

// Hyperlink wraps text in an OSC 8 hyperlink.
func Hyperlink(text string, url string) string {
	terminator := "\x1b\\"
	if strings.Contains(text, "\x07") {
		terminator = "\x07"
	}
	return "\x1b]8;;" + url + terminator + text + "\x1b]8;;" + terminator
}

// RenderLatexOptions configure the LaTeX renderer.
type RenderLatexOptions struct {
	// Display stacks fractions and operator limits vertically.
	Display bool
}

// RenderLatex renders a LaTeX math expression as terminal-friendly Unicode.
// It reports false when the expression contains unsupported or malformed
// syntax (upstream returns undefined). See latex.go: the full renderer lands
// in a later round (divergence D68).
func RenderLatex(source string, options RenderLatexOptions) (string, bool) {
	return renderLatexImpl(source, options)
}
