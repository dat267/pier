package markdown

import "strings"

// Document is an incrementally-updated markdown source. Update reuses the
// tokens of every block that appending cannot affect and re-lexes only the
// trailing run, which is what keeps a streaming transcript linear instead of
// re-lexing the whole message on every partial.
type Document struct {
	src    string
	blocks []*Block
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
		d.blocks = Lex(src)
		d.parsed = len(d.blocks)
		return d.blocks
	}
	if src == d.src {
		return d.blocks
	}
	start := FirstUnstable(d.blocks, d.src)
	offset := d.blocks[start].Start
	reused := append([]*Block(nil), d.blocks[:start]...)
	tail := (&blockLexer{src: src}).lexBlocks(offset, len(src))
	ParseInlines(tail)
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
	if from <= 0 {
		return Lex(src)
	}
	tail := (&blockLexer{src: src}).lexBlocks(from, len(src))
	ParseInlines(tail)
	return tail
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

	marker, _, ok := fenceStart(b.Raw)
	if !ok {
		return
	}
	lines := strings.Split(b.Raw, "\n")
	lastLine := lines[len(lines)-1]
	if lastLine == "" || len(lastLine) >= len(marker) || lastLine != strings.Repeat(string(marker[0]), len(lastLine)) {
		return
	}
	b.Body = strings.TrimSuffix(b.Body[:len(b.Body)-len(lastLine)], "\n")
}

// blockExtendable reports whether appending text after b can change b or merge
// it with the appended block.
func blockExtendable(b *Block, src string) bool {
	// The block ends mid-line, so the next append can extend that line.
	if blockMidLine(b, src) {
		return true
	}
	switch b.Kind {
	case BlockSpace, BlockList, BlockQuote:
		return true
	case BlockCode:
		return codeBlockOpen(b.Raw)
	}
	return false
}

// codeBlockOpen reports an indented or unclosed fenced code block (one a later
// append can still extend).
func codeBlockOpen(raw string) bool {
	marker, _, ok := fenceStart(raw)
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
