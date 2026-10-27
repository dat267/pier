package tui

import (
	"strings"

	"github.com/dat267/pier/internal/markdown"
)

// TerminalCapabilities holds the terminal feature flags the markdown renderer
// consults (upstream getCapabilities(); divergence D69: injectable instead of
// environment-detected).
type TerminalCapabilities struct {
	Hyperlinks bool
	TrueColor  bool
	// Images is the inline-image protocol ("" | "kitty" | "iterm2"); the
	// encoders and the Image component live in terminalimage.go and
	// imagecomponent.go.
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
	// ForceItalic applies italic to every rendered text element, including
	// headings and highlighted code. Opt-in for thinking blocks (D263).
	ForceItalic bool
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
	//
	// D180: upstream 54c19a252 also flattens its cached lines (flattenLines): V8
	// keeps string concatenations as ropes, and an unflattened cache retains a
	// tree of the concatenated parts. Go strings are immutable flat buffers, so
	// there is no equivalent representation to flatten.
	cacheTokenKeys  []mdBlockKey
	cacheTokenLines [][]string
	cacheWidth      int

	// Where a later render may start re-lexing. cacheWindowOffset is the byte
	// offset into cacheSource from which the last render lexed, and
	// cacheWindowBase the block index at that offset; every block before it is
	// stable while the source only grows, so its keys and final lines carry over.
	// cacheSource is the normalized source the window was computed from and the
	// prefix check that keeps the offsets honest.
	cacheWindowOffset int
	cacheWindowBase   int
	cacheSource       string

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
	bumpContentRevision()
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
	m.cacheWindowOffset = 0
	m.cacheWindowBase = 0
	m.cacheSource = ""
	bumpContentRevision()
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
		Style:                      markdownStyle{theme: m.Theme},
		Default:                    markdownDefaultStyle(m.DefaultTextStyle),
		Terminal:                   markdownTerminal{},
		Hyperlinks:                 GetTerminalCapabilities().Hyperlinks,
		PreserveOrderedListMarkers: m.Options.PreserveOrderedListMarkers,
		PreserveBackslashEscapes:   m.Options.PreserveBackslashEscapes,
		LatexEnabled:               m.renderLatexEnabled(),
		Latex: func(source string, display bool) (string, bool) {
			return RenderLatex(source, RenderLatexOptions{Display: display})
		},
		HighlightCode:   m.Theme.HighlightCode,
		CodeBlockIndent: m.Theme.CodeBlockIndent,
	}
}

// markdownStyle adapts the component's MarkdownTheme to the renderer's Style
// surface. Nil functions are identity (the fields are optional). The heading
// level composition (bold, and underline for H1) is done by the renderer, so
// the heading func is applied as-is.
type markdownStyle struct{ theme MarkdownTheme }

func (s markdownStyle) Bold(text string) string {
	if s.theme.Bold == nil {
		return text
	}
	return s.theme.Bold(text)
}

func (s markdownStyle) Italic(text string) string {
	if s.theme.Italic == nil {
		return text
	}
	return s.theme.Italic(text)
}

func (s markdownStyle) Strikethrough(text string) string {
	if s.theme.Strikethrough == nil {
		return text
	}
	return s.theme.Strikethrough(text)
}

func (s markdownStyle) Underline(text string) string {
	if s.theme.Underline == nil {
		return text
	}
	return s.theme.Underline(text)
}

func (s markdownStyle) Code(text string) string {
	if s.theme.Code == nil {
		return text
	}
	return s.theme.Code(text)
}

func (s markdownStyle) CodeBlock(text string) string {
	if s.theme.CodeBlock == nil {
		return text
	}
	return s.theme.CodeBlock(text)
}

func (s markdownStyle) CodeBlockBorder(text string) string {
	if s.theme.CodeBlockBorder == nil {
		return text
	}
	return s.theme.CodeBlockBorder(text)
}

func (s markdownStyle) Link(text string) string {
	if s.theme.Link == nil {
		return text
	}
	return s.theme.Link(text)
}

func (s markdownStyle) LinkURL(text string) string {
	if s.theme.LinkURL == nil {
		return text
	}
	return s.theme.LinkURL(text)
}

func (s markdownStyle) Heading(level int, text string) string {
	if s.theme.Heading == nil {
		return text
	}
	return s.theme.Heading(text)
}

func (s markdownStyle) Quote(text string) string {
	if s.theme.Quote == nil {
		return text
	}
	return s.theme.Quote(text)
}

func (s markdownStyle) QuoteBorder(text string) string {
	if s.theme.QuoteBorder == nil {
		return text
	}
	return s.theme.QuoteBorder(text)
}

func (s markdownStyle) Bullet(text string) string {
	if s.theme.ListBullet == nil {
		return text
	}
	return s.theme.ListBullet(text)
}

func (s markdownStyle) ThematicBreak(text string) string {
	if s.theme.Hr == nil {
		return text
	}
	return s.theme.Hr(text)
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
		m.cacheWindowOffset = 0
		m.cacheWindowBase = 0
		m.cacheSource = ""
		return m.cachedLines
	}

	normalizedText := strings.ReplaceAll(text, "\t", "   ")

	// Incremental lex. When the source only grew since the last render, the
	// blocks before the trailing open run are unchanged, so re-lex only the tail
	// (markdown.LexFrom) and carry their keys and final lines forward; otherwise
	// lex from the start. No block tree is retained between renders: the keys
	// hold the deciding fields and reference the cached source, exactly like the
	// line cache.
	windowBase, windowOffset := 0, 0
	if m.canReuseLex(normalizedText, width) {
		windowBase, windowOffset = m.cacheWindowBase, m.cacheWindowOffset
	}
	blocks := markdown.LexFrom(normalizedText, windowOffset)
	markdown.TrimPartialClosingFences(blocks)
	total := windowBase + len(blocks)

	// Reuse every block before the re-lex window (whose source is byte-identical)
	// and extend the reuse through the re-lexed tail by key comparison. A block's
	// lines were rendered with the following block as context, so the last
	// matching block is only reused when its successor also matches.
	//
	// The block keys are width-independent, so a matching key does not mean the
	// cached lines are: they were rendered at m.cacheWidth. Reusing them across a
	// width change published old-width lines (the terminal then truncated the
	// frame). Guard the reuse on the cached render's width; canReuseLex already
	// requires it for the window path, but the key path is independent.
	cacheMatchesWidth := m.cacheWidth == width
	matched := matchBlockKeys(blocks, windowBase, total, m.cacheTokenKeys)
	if !cacheMatchesWidth {
		matched = windowBase
	}
	if windowBase > 0 && matched == windowBase {
		// The stable-prefix invariant did not hold; redo the whole source.
		// canReuseLex required the same width, so the recomputed match is safe.
		windowBase, windowOffset = 0, 0
		blocks = markdown.LexFrom(normalizedText, 0)
		markdown.TrimPartialClosingFences(blocks)
		total = len(blocks)
		matched = matchBlockKeys(blocks, 0, total, m.cacheTokenKeys)
	}
	reuse := max(min(max(matched-1, windowBase), total-1), 0)

	renderer := markdown.NewRenderer(m.renderOptions())
	finals := make([][]string, total)
	for index := range reuse {
		finals[index] = m.cacheTokenLines[index]
	}
	for index := reuse; index < total; index++ {
		var next *markdown.Block
		if index+1 < total {
			next = blocks[index+1-windowBase]
		}
		m.renderedTokens++
		rendered := renderer.RenderBlock(blocks[index-windowBase], contentWidth, next)
		finals[index] = renderer.Finalize(rendered, width, contentWidth, m.PaddingX)
		if m.Options.ForceItalic {
			for lineIndex, line := range finals[index] {
				finals[index][lineIndex] = forceMarkdownItalic(line)
			}
		}
	}

	var contentLines []string
	for _, final := range finals {
		contentLines = append(contentLines, final...)
	}
	if len(finals) > 0 {
		m.cacheTokenLines = finals[:len(finals)-1]
	} else {
		m.cacheTokenLines = nil
	}

	fullKeys := make([]mdBlockKey, total)
	copy(fullKeys, m.cacheTokenKeys[:min(windowBase, len(m.cacheTokenKeys))])
	copy(fullKeys[windowBase:], markdownBlockKeys(blocks))
	m.cacheTokenKeys = fullKeys
	m.cacheWidth = width
	m.cacheSource = normalizedText

	// Record where the next render can begin re-lexing: the block just before
	// the first one an append can still change. Only the tail is in hand, so the
	// window never moves behind the current base.
	nextBase := 0
	if len(blocks) > 0 {
		nextBase = min(max(windowBase+markdown.FirstUnstable(blocks, normalizedText)-1, windowBase), total-1)
	}
	m.cacheWindowBase = nextBase
	if nextBase <= 0 {
		m.cacheWindowBase = 0
		m.cacheWindowOffset = 0
	} else {
		m.cacheWindowOffset = fullKeys[nextBase].Start
	}

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

// canReuseLex reports whether the previous render's lex can seed this one: the
// source only grew (so the cached offsets and keys stay valid) and the width is
// unchanged (so the cached lines can be reused). A transform can rewrite the
// source non-monotonically, so it always re-lexes from the start.
func (m *Markdown) canReuseLex(normalizedText string, width int) bool {
	if m.cacheWindowOffset <= 0 || m.cacheWindowBase <= 0 {
		return false
	}
	if m.Options.Transform != nil || m.cacheWidth != width || m.cacheSource == "" {
		return false
	}
	if m.cacheWindowOffset > len(normalizedText) || m.cacheWindowBase > len(m.cacheTokenKeys) {
		return false
	}
	return strings.HasPrefix(normalizedText, m.cacheSource)
}

// matchBlockKeys returns how many leading blocks of the re-lexed tail agree with
// the cached keys, starting at the reused prefix's base.
func matchBlockKeys(blocks []*markdown.Block, base int, total int, previous []mdBlockKey) int {
	limit := min(len(previous), total)
	matched := base
	for matched < limit && sameMdBlockKey(blocks[matched-base], previous[matched]) {
		matched++
	}
	return matched
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
