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
	start := len(d.blocks) - 1
	for start > 0 && (blockExtendable(d.blocks[start-1], d.src) || blockMidLine(d.blocks[start], d.src)) {
		start--
	}
	offset := d.blocks[start].Start
	reused := append([]*Block(nil), d.blocks[:start]...)
	tail := (&blockLexer{src: src}).lexBlocks(offset, len(src))
	ParseInlines(tail)
	d.src = src
	d.blocks = append(reused, tail...)
	d.parsed = len(d.blocks)
	return d.blocks
}

// blockMidLine reports a block whose raw ends without a newline: the next
// append can turn its last (whitespace-only) line into content.
func blockMidLine(b *Block, src string) bool {
	return b.End > 0 && b.End <= len(src) && src[b.End-1] != '\n'
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
