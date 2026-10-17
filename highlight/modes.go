package highlight

// Common modes and patterns exported by the reference core (core.js), reused by
// language definitions.

const (
	// IdentRe matches an identifier.
	IdentRe = `[a-zA-Z]\w*`
	// UnderscoreIdentRe matches a language identifier that may start with `_`.
	UnderscoreIdentRe = `[a-zA-Z_]\w*`
	// NumberRe is the simple number pattern.
	NumberRe = `\b\d+(\.\d+)?`
	// CNumberRe is the C-style number pattern.
	CNumberRe = `(-?)(\b0[xX][a-fA-F0-9]+|(\b\d+(\.\d*)?|\.\d+)([eE][-+]?\d+)?)`
	// BinaryNumberRe is the binary literal pattern.
	BinaryNumberRe = `\b(0b[01]+)`
	// MatchNothingRe never matches.
	MatchNothingRe = `\b\B`
)

// BackslashEscape matches a backslash escape.
var BackslashEscape = &Mode{Begin: `\\[\s\S]`, HasRelevance: true, Relevance: 0}

// AposStringMode matches a single-quoted string.
var AposStringMode = &Mode{
	ClassName: "string",
	Begin:     `'`,
	End:       `'`,
	Illegal:   `\n`,
	Contains:  []*Mode{BackslashEscape},
}

// QuoteStringMode matches a double-quoted string.
var QuoteStringMode = &Mode{
	ClassName: "string",
	Begin:     `"`,
	End:       `"`,
	Illegal:   `\n`,
	Contains:  []*Mode{BackslashEscape},
}

// PhrasalWordsMode matches common English filler words in comments.
var PhrasalWordsMode = &Mode{
	Begin: `\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\b`,
}

func commentMode(begin, end string) *Mode {
	return &Mode{
		ClassName: "comment",
		Begin:     begin,
		End:       end,
		Contains: []*Mode{
			PhrasalWordsMode,
			Zero(Mode{ClassName: "doctag", Begin: `(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):`}),
		},
	}
}

// CLineCommentMode matches `// ...` comments.
var CLineCommentMode = commentMode(`//`, `$`)

// CBlockCommentMode matches `/* ... */` comments.
var CBlockCommentMode = commentMode(`/\*`, `\*/`)

// HashCommentMode matches `# ...` comments.
var HashCommentMode = commentMode(`#`, `$`)

// NumberMode matches a number.
var NumberMode = Zero(Mode{ClassName: "number", Begin: NumberRe})

// CNumberMode matches a C-style number.
var CNumberMode = Zero(Mode{ClassName: "number", Begin: CNumberRe})

// BinaryNumberMode matches a binary literal.
var BinaryNumberMode = Zero(Mode{ClassName: "number", Begin: BinaryNumberRe})

// TitleMode matches a function/class title.
var TitleMode = Zero(Mode{ClassName: "title", Begin: IdentRe})

// UnderscoreTitleMode matches a function/class title that may start with `_`.
var UnderscoreTitleMode = Zero(Mode{ClassName: "title", Begin: UnderscoreIdentRe})

// EndSameAsBegin adds the reference's END_SAME_AS_BEGIN mechanics: the mode's
// first capture group at `begin` must equal the first group at `end`.
func EndSameAsBegin(mode *Mode) *Mode {
	mode.OnBegin = func(m *match, resp *Response) {
		resp.data["_beginMatch"] = m.group(1)
	}
	mode.OnEnd = func(m *match, resp *Response) {
		if resp.data["_beginMatch"] != m.group(1) {
			resp.IgnoreMatch()
		}
	}
	return mode
}

// Shebang builds the reference's SHEBANG mode. binary, when set, narrows the
// match to a known interpreter and boosts relevance.
func Shebang(binary string, relevance int) *Mode {
	begin := `^#![ ]*/`
	if binary != "" {
		begin = begin + `.*` + binary + `.*`
	}
	mode := &Mode{
		ClassName:    "meta",
		Begin:        begin,
		End:          `$`,
		HasRelevance: true,
		Relevance:    relevance,
	}
	mode.OnBegin = func(m *match, resp *Response) {
		if m.index != 0 {
			resp.IgnoreMatch()
		}
	}
	return mode
}

// MethodGuard excludes method names from keyword processing.
var MethodGuard = Zero(Mode{Begin: `\.\s*` + UnderscoreIdentRe})

// CSSNumberMode matches a CSS number with an optional unit (core.js).
var CSSNumberMode = Zero(Mode{
	ClassName: "number",
	Begin:     NumberRe + "(%|em|ex|ch|rem|vw|vh|vmin|vmax|cm|mm|in|pt|pc|px|deg|grad|rad|turn|s|ms|Hz|kHz|dpi|dpcm|dppx)?",
})
