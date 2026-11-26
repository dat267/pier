package highlight

func init() { RegisterLanguage("scala", Scala()) }

// Scala returns the Scala language definition (a port of languages/scala.js).
func Scala() *Language {
	annotation := &Mode{ClassName: "meta", Begin: `@[A-Za-z]+`}

	subst := &Mode{
		ClassName: "subst",
		Variants: []*Mode{
			{ClassName: "subst", Begin: `\$[A-Za-z0-9_]+`},
			{ClassName: "subst", Begin: `\$\{`, End: `\}`},
		},
	}

	stringMode := &Mode{
		ClassName: "string",
		Variants: []*Mode{
			{ClassName: "string", Begin: `"""`, End: `"""`},
			{ClassName: "string", Begin: `"`, End: `"`, Illegal: `\n`, Contains: []*Mode{BackslashEscape}},
			{ClassName: "string", Begin: `[a-z]+"`, End: `"`, Illegal: `\n`, Contains: []*Mode{BackslashEscape, subst}},
			{ClassName: "string", Begin: `[a-z]+"""`, End: `"""`, Contains: []*Mode{subst}, HasRelevance: true, Relevance: 10},
		},
	}

	// The reference's `'\w[\w\d_]*(?!\')` is a BACKTRACKING lookahead, not a
	// veto: on `'str'` the greedy body fails the lookahead, so the engine retries
	// a shorter one and the rule matches `'st`, leaving `r'` as plain text. A
	// beforeBegin guard cannot express that (the lexeme is captured before it
	// runs), so the pattern is matched by hand and shortened the same way. A
	// single-character symbol cannot shorten: `'\w` consumes the only word
	// character, so `'b'` matches nothing at all.
	symbol := &Mode{ClassName: "symbol", ManualFirst: "'", ManualBegin: scalaSymbolBegin}

	typeMode := Zero(Mode{ClassName: "type", Begin: `\b[A-Z][A-Za-z0-9_]*`})
	// The NAME pattern contains a backtick, so it cannot be a Go raw string.
	nameBegin := "[^0-9\\n\\t \"'(),.`{}\\[\\]:;][^\\n\\t \"'(),.`{}\\[\\]:;]+|[^0-9\\n\\t \"'(),.`{}\\[\\]:;=]"
	name := Zero(Mode{ClassName: "title", Begin: nameBegin})

	classMode := &Mode{
		ClassName:     "class",
		BeginKeywords: "class object trait type",
		End:           `[:={\[\n;]`,
		ExcludeEnd:    true,
		Contains: []*Mode{
			CLineCommentMode,
			CBlockCommentMode,
			{BeginKeywords: "extends with", HasRelevance: true, Relevance: 10},
			Zero(Mode{Begin: `\[`, End: `\]`, ExcludeBegin: true, ExcludeEnd: true, Contains: []*Mode{typeMode}}),
			Zero(Mode{ClassName: "params", Begin: `\(`, End: `\)`, ExcludeBegin: true, ExcludeEnd: true, Contains: []*Mode{typeMode}}),
			name,
		},
	}
	method := &Mode{
		ClassName:     "function",
		BeginKeywords: "def",
		End:           `[:={\[(\n;]`,
		ExcludeEnd:    true,
		Contains:      []*Mode{name},
	}

	return &Language{
		Name: "Scala",
		Keywords: Keywords{
			"literal": "true false null",
			"keyword": "type yield lazy override def with val var sealed abstract private trait object if forSome for while throw finally protected extends import final return else break new catch super class case package default try this match continue throws implicit",
		},
		Contains: []*Mode{
			CLineCommentMode,
			CBlockCommentMode,
			stringMode,
			symbol,
			typeMode,
			method,
			classMode,
			CNumberMode,
			annotation,
		},
	}
}

// scalaSymbolBegin ports `'\w[\w\d_]*(?!\')` with the backtracking the lookahead
// implies: the longest prefix whose following byte is not a quote, or no match
// when even the shortest form is followed by one.
func scalaSymbolBegin(s string, pos int) (int, bool) {
	if pos >= len(s) || s[pos] != '\'' {
		return 0, false
	}
	end := pos + 1
	if end >= len(s) || !isWordByte(s[end]) {
		return 0, false
	}
	end++
	for end < len(s) && isWordByte(s[end]) {
		end++
	}
	for end > pos+2 {
		if end >= len(s) || s[end] != '\'' {
			return end, true
		}
		end--
	}
	if end >= len(s) || s[end] != '\'' {
		return end, true
	}
	return 0, false
}
