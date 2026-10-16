// Package markdown is a modular, incremental Markdown renderer for the port's
// terminal UI. It is built from the ground up rather than ported from Marked:
// the block lexer, inline parser, layout and renderer are separate stages over
// a small document model, and none of them uses regexp (the previous lexer
// called a dozen compiled regexes per line, which is where its throughput went).
//
// The model keeps each top-level block's byte range so the lexer can be driven
// incrementally: appending to a source reuses the blocks before the trailing
// open block and re-lexes only the tail.
package markdown

// BlockKind identifies a block-level element.
type BlockKind uint8

const (
	BlockParagraph BlockKind = iota
	BlockHeading
	BlockCode
	BlockQuote
	BlockList
	BlockThematicBreak
	BlockTable
	BlockHTML
	BlockListItem
)

// Block is a block-level element. Blocks form a tree: BlockQuote and BlockList
// carry children (BlockList's children are BlockListItem-shaped Blocks).
type Block struct {
	Kind BlockKind

	// Start and End are byte offsets into the source. End is exclusive and
	// covers the block's raw text including its trailing newline.
	Start int
	End   int

	// Level is an ATX/setext heading level (1-6).
	Level int
	// Info is a fenced code block's info string (language).
	Info string
	// Body is a code block's text, or an HTML block's raw source.
	Body string
	// Text is a paragraph's or heading's inline source, or a table cell's.
	Text string
	// Inlines is the parsed inline content of Text.
	Inlines []Inline

	// Blocks are a quote's or list item's children.
	Blocks []*Block

	// List fields.
	Ordered     bool
	StartNumber int
	Tight       bool

	// Table fields: Align per column, Header cells, Rows of cells.
	Align  []Align
	Header []string
	Rows   [][]string
	// headerInlines and rowInlines are filled by ParseInlines.
	headerInlines [][]Inline
	rowInlines    [][][]Inline

	// Task marks a task-list item's checkbox state.
	Task    *bool
	checked bool
}

// Align is a table column alignment.
type Align uint8

const (
	AlignNone Align = iota
	AlignLeft
	AlignCenter
	AlignRight
)

// InlineKind identifies an inline element.
type InlineKind uint8

const (
	InlineText InlineKind = iota
	InlineCode
	InlineStrong
	InlineEmphasis
	InlineStrikethrough
	InlineLink
	InlineImage
	InlineHardBreak
	InlineSoftBreak
)

// Inline is an inline element.
type Inline struct {
	Kind InlineKind
	// Text is the literal for InlineText and InlineCode.
	Text string
	// Children are the nested inlines for emphasis, strong, strikethrough,
	// links and images.
	Children []Inline
	// Dest and Title are a link's or image's destination and title.
	Dest  string
	Title string
}

// Paragraph returns a plain paragraph block with the given inline source.
func paragraph(text string) *Block { return &Block{Kind: BlockParagraph, Text: text} }
