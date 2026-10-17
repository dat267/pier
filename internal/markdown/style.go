package markdown

// Style is the renderer's styling surface: one method per markdown element it
// paints. It is deliberately narrow and independent of any terminal library, so
// the same renderer can be driven by a TUI theme adapter, an ANSI style, or a
// no-op style for tests and measurement. There is one renderer, so there is one
// styling surface (the previous Theme function-field surface is gone).
type Style interface {
	Bold(text string) string
	Italic(text string) string
	Strikethrough(text string) string
	Underline(text string) string
	Code(text string) string
	CodeBlock(text string) string
	CodeBlockBorder(text string) string
	Link(text string) string
	LinkURL(text string) string
	Heading(level int, text string) string
	Quote(text string) string
	QuoteBorder(text string) string
	Bullet(text string) string
	ThematicBreak(text string) string
}

// NopStyle is an identity style. It is the default: the rendered text is
// exactly the source text with markup markers removed, which makes output
// assertions read naturally.
type NopStyle struct{}

func (NopStyle) Bold(text string) string               { return text }
func (NopStyle) Italic(text string) string             { return text }
func (NopStyle) Strikethrough(text string) string      { return text }
func (NopStyle) Underline(text string) string          { return text }
func (NopStyle) Code(text string) string               { return text }
func (NopStyle) CodeBlock(text string) string          { return text }
func (NopStyle) CodeBlockBorder(text string) string    { return text }
func (NopStyle) Link(text string) string               { return text }
func (NopStyle) LinkURL(text string) string            { return text }
func (NopStyle) Heading(level int, text string) string { return text }
func (NopStyle) Quote(text string) string              { return text }
func (NopStyle) QuoteBorder(text string) string        { return text }
func (NopStyle) Bullet(text string) string             { return text }
func (NopStyle) ThematicBreak(text string) string      { return text }
