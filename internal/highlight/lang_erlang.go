package highlight

func init() { RegisterLanguage("erlang", Erlang()) }

// Erlang returns the Erlang language definition (a port of languages/erlang.js).
// Its block, tuple and record modes are mutually recursive, so the modes are
// linked with pointers after construction.
func Erlang() *Language {
	const basicAtomRe = `[a-z'][a-zA-Z0-9_']*`
	functionNameRe := `(` + basicAtomRe + `:` + basicAtomRe + `|` + basicAtomRe + `)`

	reserved := Keywords{
		"keyword": "after and andalso|10 band begin bnot bor bsl bzr bxor case catch cond div end fun if " +
			"let not of orelse|10 query receive rem try when xor",
		"literal": "false true",
	}

	comment := CommentMode(`%`, `$`)
	number := &Mode{
		ClassName:    "number",
		Begin:        `\b(\d+(_\d+)*#[a-fA-F0-9]+(_[a-fA-F0-9]+)*|\d+(_\d+)*(\.\d+(_\d+)*)?([eE][-+]?\d+)?)`,
		HasRelevance: true,
		Relevance:    0,
	}
	namedFun := &Mode{Begin: `fun\s+` + basicAtomRe + `/\d+`}

	functionCall := &Mode{
		Begin:        functionNameRe + `\(`,
		End:          `\)`,
		ReturnBegin:  true,
		HasRelevance: true,
		Relevance:    0,
		Contains: []*Mode{
			{Begin: functionNameRe, HasRelevance: true, Relevance: 0},
			{
				Begin:          `\(`,
				End:            `\)`,
				EndsWithParent: true,
				ReturnEnd:      true,
				HasRelevance:   true,
				Relevance:      0,
			},
		},
	}
	tuple := &Mode{Begin: `\{`, End: `\}`, HasRelevance: true, Relevance: 0}
	var1 := &Mode{Begin: `\b_([A-Z][A-Za-z0-9_]*)?`, HasRelevance: true, Relevance: 0}
	var2 := &Mode{Begin: `[A-Z][a-zA-Z0-9_]*`, HasRelevance: true, Relevance: 0}
	recordAccess := &Mode{
		Begin:        `#` + UnderscoreIdentRe,
		HasRelevance: true,
		Relevance:    0,
		ReturnBegin:  true,
		Contains: []*Mode{
			{Begin: `#` + UnderscoreIdentRe, HasRelevance: true, Relevance: 0},
			{Begin: `\{`, End: `\}`, HasRelevance: true, Relevance: 0},
		},
	}

	blockStatements := &Mode{
		BeginKeywords: "fun receive if try case",
		End:           `end`,
		Keywords:      reserved,
	}
	blockStatements.Contains = []*Mode{
		comment,
		namedFun,
		Variant(AposStringMode, func(m *Mode) { m.ClassName = "" }),
		blockStatements,
		functionCall,
		QuoteStringMode,
		number,
		tuple,
		var1,
		var2,
		recordAccess,
	}

	basicModes := []*Mode{
		comment,
		namedFun,
		blockStatements,
		functionCall,
		QuoteStringMode,
		number,
		tuple,
		var1,
		var2,
		recordAccess,
	}
	functionCall.Contains[1].Contains = basicModes
	tuple.Contains = basicModes
	recordAccess.Contains[1].Contains = basicModes

	params := &Mode{ClassName: "params", Begin: `\(`, End: `\)`, Contains: basicModes}

	function := &Mode{
		ClassName:   "function",
		Begin:       `^` + basicAtomRe + `\s*\(`,
		End:         `->`,
		ReturnBegin: true,
		Illegal:     `\(|#|//|/\*|\\|:|;`,
		Contains: []*Mode{
			params,
			Variant(TitleMode, func(m *Mode) { m.Begin = basicAtomRe }),
		},
		Starts: &Mode{
			End:      `;|\.`,
			Keywords: reserved,
			Contains: basicModes,
		},
	}

	return &Language{
		Name:     "Erlang",
		Aliases:  []string{"erl"},
		Keywords: reserved,
		Illegal:  `(</|\*=|\+=|-=|/\*|\*/|\(\*|\*\))`,
		Contains: []*Mode{
			function,
			comment,
			{
				Begin:        `^-`,
				End:          `\.`,
				HasRelevance: true,
				Relevance:    0,
				ExcludeEnd:   true,
				ReturnBegin:  true,
				Keywords: Keywords{
					"$pattern": `-` + IdentRe,
					"keyword": "-module|1.5 -record|1.5 -undef|1.5 -export|1.5 -ifdef|1.5 -ifndef|1.5 " +
						"-author|1.5 -copyright|1.5 -doc|1.5 -vsn|1.5 -import|1.5 -include|1.5 " +
						"-include_lib|1.5 -compile|1.5 -define|1.5 -else|1.5 -endif|1.5 -file|1.5 " +
						"-behaviour|1.5 -behavior|1.5 -spec|1.5",
				},
				Contains: []*Mode{params},
			},
			number,
			QuoteStringMode,
			recordAccess,
			var1,
			var2,
			tuple,
			{Begin: `\.$`},
		},
	}
}
