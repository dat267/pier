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

	// The reference's `'\w[\w\d_]*(?!\')` lookahead becomes a guard.
	symbol := &Mode{ClassName: "symbol", Begin: `'\w[\w\d_]*`}
	symbol.beforeBegin = func(m *match, resp *Response) {
		if end := m.index + len(m.lexeme); end < len(m.input) && m.input[end] == '\'' {
			resp.IgnoreMatch()
		}
	}

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
