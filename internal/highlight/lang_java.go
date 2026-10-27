package highlight

func init() { RegisterLanguage("java", Java()) }

func javaNumeric() *Mode {
	const decimalDigits = `[0-9](_*[0-9])*`
	frac := `\.(` + decimalDigits + `)`
	const hexDigits = `[0-9a-fA-F](_*[0-9a-fA-F])*`
	return Zero(Mode{
		ClassName: "number",
		Variants: []*Mode{
			{ClassName: "number", Begin: `(\b(` + decimalDigits + `)((` + frac + `)|\.)?|(` + frac + `))[eE][+-]?(` + decimalDigits + `)[fFdD]?\b`},
			{ClassName: "number", Begin: `\b(` + decimalDigits + `)((` + frac + `)[fFdD]?\b|\.([fFdD]\b)?)`},
			{ClassName: "number", Begin: `(` + frac + `)[fFdD]?\b`},
			{ClassName: "number", Begin: `\b(` + decimalDigits + `)[fFdD]\b`},
			{ClassName: "number", Begin: `\b0[xX]((` + hexDigits + `)\.?|(` + hexDigits + `)?\.(` + hexDigits + `))[pP][+-]?(` + decimalDigits + `)[fFdD]?\b`},
			{ClassName: "number", Begin: `\b(0|[1-9](_*[0-9])*)[lL]?\b`},
			{ClassName: "number", Begin: `\b0[xX](` + hexDigits + `)[lL]?\b`},
			{ClassName: "number", Begin: `\b0(_*[0-7])*[lL]?\b`},
			{ClassName: "number", Begin: `\b0[bB][01](_*[01])*[lL]?\b`},
		},
	})
}

// Java returns the Java language definition (a port of languages/java.js).
func Java() *Language {
	const javaIdentRe = `[\u00C0-\u02B8a-zA-Z_$][\u00C0-\u02B8a-zA-Z_$0-9]*`
	genericIdentRe := javaIdentRe + `(<` + javaIdentRe + `(\s*,\s*` + javaIdentRe + `)*>)?`
	keywords := "false synchronized int abstract float private char boolean var static null if const " +
		"for true while long strictfp finally protected import native final void " +
		"enum else break transient catch instanceof byte super volatile case assert short " +
		"package default double public try this switch continue throws protected public private " +
		"module requires exports do"

	paren := &Mode{Begin: `\(`, End: `\)`}
	paren.Contains = []*Mode{Self}
	annotation := &Mode{
		ClassName: "meta",
		Begin:     `@` + javaIdentRe,
		Contains:  []*Mode{paren},
	}

	javadoc := Zero(Mode{
		ClassName: "comment",
		Begin:     `/\*\*`,
		End:       `\*/`,
		Contains: []*Mode{
			Zero(Mode{Begin: `\w+@`}),
			{ClassName: "doctag", Begin: `@[A-Za-z]+`},
		},
	})
	// The reference builds this with COMMENT(), which adds the phrasal-words and
	// doctag rules; the explicit javadoc contains above replace them.
	_ = HashCommentMode

	number := javaNumeric()
	numberDupe := javaNumeric()

	classMode := &Mode{
		ClassName:     "class",
		BeginKeywords: "class interface enum",
		End:           `[{;=]`,
		ExcludeEnd:    true,
		Relevance:     1,
		HasRelevance:  true,
		Keywords:      Keywords{"keyword": "class interface enum"},
		Illegal:       `[:"\[\]]`,
		Contains: []*Mode{
			{BeginKeywords: "extends implements"},
			UnderscoreTitleMode,
		},
	}
	recordMode := &Mode{
		ClassName:   "class",
		Begin:       `record\s+` + UnderscoreIdentRe + `\s*\(`,
		ReturnBegin: true,
		ExcludeEnd:  true,
		End:         `[{;=]`,
		Keywords:    Keywords{"keyword": keywords},
		Contains: []*Mode{
			{BeginKeywords: "record"},
			{Begin: UnderscoreIdentRe + `\s*\(`, ReturnBegin: true, Contains: []*Mode{UnderscoreTitleMode}},
			{
				ClassName: "params",
				Begin:     `\(`,
				End:       `\)`,
				Keywords:  Keywords{"keyword": keywords},
				Contains:  []*Mode{CBlockCommentMode},
			},
			CLineCommentMode,
			CBlockCommentMode,
		},
	}
	functionMode := &Mode{
		ClassName:   "function",
		Begin:       `(` + genericIdentRe + `\s+)+` + UnderscoreIdentRe + `\s*\(`,
		ReturnBegin: true,
		End:         `[{;=]`,
		ExcludeEnd:  true,
		Keywords:    Keywords{"keyword": keywords},
		Contains: []*Mode{
			{Begin: UnderscoreIdentRe + `\s*\(`, ReturnBegin: true, Contains: []*Mode{UnderscoreTitleMode}},
			{
				ClassName: "params",
				Begin:     `\(`,
				End:       `\)`,
				Keywords:  Keywords{"keyword": keywords},
				Contains: []*Mode{
					annotation,
					AposStringMode,
					QuoteStringMode,
					numberDupe,
					CBlockCommentMode,
				},
			},
			CLineCommentMode,
			CBlockCommentMode,
		},
	}

	importMode := &Mode{
		Begin:        `import java\.[a-z]+\.`,
		Keywords:     Keywords{"keyword": "import"},
		Relevance:    2,
		HasRelevance: true,
	}

	return &Language{
		Name:     "Java",
		Aliases:  []string{"jsp"},
		Keywords: Keywords{"keyword": keywords},
		Illegal:  `</|#`,
		Contains: []*Mode{
			javadoc,
			importMode,
			CLineCommentMode,
			CBlockCommentMode,
			AposStringMode,
			QuoteStringMode,
			classMode,
			{BeginKeywords: "new throw return else", HasRelevance: true, Relevance: 0},
			recordMode,
			functionMode,
			number,
			annotation,
		},
	}
}
