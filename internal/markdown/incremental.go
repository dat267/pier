package markdown

import (
	"slices"
	"strings"
)

// Document is an incrementally-updated markdown source. Update reuses the
// tokens of every block that appending cannot affect and re-lexes only the
// trailing run, which is what keeps a streaming transcript linear instead of
// re-lexing the whole message on every partial.
type Document struct {
	src    string
	blocks []*Block
	defs   []Definition
	parsed int // blocks[:parsed] have inline content resolved
}

// NewDocument creates an empty document.
func NewDocument() *Document { return &Document{} }

// Source returns the current source.
func (d *Document) Source() string { return d.src }

// Blocks returns the current top-level blocks, with inline content resolved.
func (d *Document) Blocks() []*Block { return d.blocks }

// Update advances the document to src, which must extend the current source (a
// non-extending source falls back to a full lex). It returns the current blocks.
func (d *Document) Update(src string) []*Block {
	if !strings.HasPrefix(src, d.src) || d.blocks == nil {
		d.src = src
		d.blocks, d.defs = LexDefinitions(src, 0, nil)
		d.parsed = len(d.blocks)
		return d.blocks
	}
	if src == d.src {
		return d.blocks
	}
	start := FirstUnstable(d.blocks, d.src)
	offset := d.blocks[start].Start
	reused := append([]*Block(nil), d.blocks[:start]...)
	tail, defs := LexDefinitions(src, offset, DefinitionsBefore(d.defs, offset))
	if !SameDefinitions(defs, d.defs) {
		// The reused blocks resolved their reference links without the new
		// definitions.
		d.src = ""
		d.blocks = nil
		return d.Update(src)
	}
	d.src = src
	d.blocks = append(reused, tail...)
	d.parsed = len(d.blocks)
	return d.blocks
}

// FirstUnstable returns the index of the first block an append to src can still
// change. Blocks before it are stable: as long as the source only grows they
// keep their kind, content and byte offsets, and the block just before them has
// a stable successor. src is the source the blocks were lexed from.
//
// It is the incremental contract shared by Document.Update and callers that
// re-lex only a tail (see LexFrom).
func FirstUnstable(blocks []*Block, src string) int {
	start := len(blocks) - 1
	for start > 0 && (blockExtendable(blocks[start-1], src) || blockMidLine(blocks[start], src)) {
		start--
	}
	if start < 0 {
		return 0
	}
	return start
}

// LexFrom lexes src from byte offset from to its end, reporting absolute block
// offsets, and resolves the new tail's inline content. Blocks wholly before
// from are assumed unchanged, so a caller that knows from is stable (see
// FirstUnstable) can re-lex only the tail and keep the earlier blocks' keys and
// rendered lines. from at or below zero is a full Lex.
func LexFrom(src string, from int) []*Block {
	blocks, _ := LexDefinitions(src, from, nil)
	return blocks
}

// LexDefinitions is LexFrom for a source with link reference definitions:
// prior holds the definitions the blocks before from keep (a previous call's
// result whose Offset is below from), and the result holds them followed by
// the tail's. Marked resolves reference links once every block is lexed, so the
// tail's inline content sees every definition, and a definition in the tail can
// change how a block before from renders: a caller reusing those blocks'
// lines must compare the results (SameDefinitions).
func LexDefinitions(src string, from int, prior []Definition) ([]*Block, []Definition) {
	from = max(from, 0)
	blocks := (&blockLexer{src: src}).lexBlocks(from, len(src))
	defs := prior
	if mayHoldDefinition(src[from:]) {
		blocks, defs = collectDefinitions(blocks, slices.Clip(prior))
	}
	parseInlines(blocks, &inlineState{links: linksFor(defs)})
	return blocks, defs
}

// DefinitionsBefore returns the definitions held by blocks starting before
// offset.
func DefinitionsBefore(defs []Definition, offset int) []Definition {
	n := 0
	for n < len(defs) && defs[n].Offset < offset {
		n++
	}
	return defs[:n:n]
}

// SameDefinitions reports whether two definition lists resolve every label
// the same way.
func SameDefinitions(a, b []Definition) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Label != b[i].Label || a[i].Href != b[i].Href || a[i].Title != b[i].Title {
			return false
		}
	}
	return true
}

// blockMidLine reports a block whose raw ends without a newline: the next
// append can turn its last (whitespace-only) line into content.
func blockMidLine(b *Block, src string) bool {
	return b.End > 0 && b.End <= len(src) && src[b.End-1] != '\n'
}

// TrimPartialClosingFences mirrors markdown.ts's trimPartialClosingFences: it
// removes a streamed, still-incomplete closing fence line from the last code
// block so a partial message does not render a marker the user has not finished
// typing.
func TrimPartialClosingFences(blocks []*Block) {
	if len(blocks) == 0 {
		return
	}
	b := blocks[len(blocks)-1]
	switch b.Kind {
	case BlockList:
		if len(b.Blocks) > 0 {
			TrimPartialClosingFences(b.Blocks[len(b.Blocks)-1].Blocks)
		}
		return
	case BlockQuote:
		TrimPartialClosingFences(b.Blocks)
		return
	case BlockCode:
	default:
		return
	}

	marker, ok := fenceMarkerOf(b.Raw)
	if !ok {
		return
	}
	lines := strings.Split(b.Raw, "\n")
	lastLine := lines[len(lines)-1]
	if lastLine == "" || len(lastLine) >= len(marker) || lastLine != strings.Repeat(string(marker[0]), len(lastLine)) {
		return
	}
	// The reference slices with `text.slice(0, -lastLine.length)`, which clamps
	// to "" when the body is shorter; an unclamped slice panicked there.
	cut := max(len(b.Body)-len(lastLine), 0)
	b.Body = strings.TrimSuffix(b.Body[:cut], "\n")
}

// fenceMarkerOf returns the opening fence marker of a block's raw source. The
// reference reads it from the start of token.raw with /^(`{3,}|~{3,})/, so only
// the first line decides: the raw's later lines, including a streamed partial
// closing fence, are not the opening line's info string.
func fenceMarkerOf(raw string) (string, bool) {
	first, _, _ := strings.Cut(raw, "\n")
	marker, _, ok := fenceStart(first)
	return marker, ok
}

// blockExtendable reports whether appending text after b can change b or merge
// it with the appended block.
func blockExtendable(b *Block, src string) bool {
	// The block ends mid-line, so the next append can extend that line.
	if blockMidLine(b, src) {
		return true
	}
	switch b.Kind {
	case BlockSpace, BlockList, BlockQuote, BlockLinkDef:
		return true
	case BlockCode:
		return codeBlockOpen(b.Raw)
	}
	return false
}

// codeBlockOpen reports an indented or unclosed fenced code block (one a later
// append can still extend).
func codeBlockOpen(raw string) bool {
	marker, ok := fenceMarkerOf(raw)
	if !ok {
		return true // indented code
	}
	trimmed := strings.TrimRight(raw, "\n")
	last := trimmed
	if nl := strings.LastIndexByte(trimmed, '\n'); nl >= 0 {
		last = trimmed[nl+1:]
	}
	return !isFenceEnd(last, marker)
}
