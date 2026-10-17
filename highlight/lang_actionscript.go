package highlight

func init() { RegisterLanguage("actionscript", ActionScript()) }

// ActionScript returns the ActionScript language definition (a port of
// languages/actionscript.js).
func ActionScript() *Language {
	const identRe = `[a-zA-Z_$][a-zA-Z0-9_$]*`
	const returnTypeRe = `([*]|[a-zA-Z_$][a-zA-Z0-9_$]*)`

	restArg := &Mode{
		ClassName:    "rest_arg",
		Begin:        `[.]{3}`,
		End:          identRe,
		HasRelevance: true,
		Relevance:    10,
	}

	params := &Mode{
		ClassName: "params",
		Begin:     `\(`,
		End:       `\)`,
		Contains: []*Mode{
			AposStringMode,
			QuoteStringMode,
			CLineCommentMode,
			CBlockCommentMode,
			restArg,
		},
	}

	function := &Mode{
		ClassName:     "function",
		BeginKeywords: "function",
		End:           `[{;]`,
		ExcludeEnd:    true,
		Illegal:       `\S`,
		Contains: []*Mode{
			TitleMode,
			params,
			{Begin: `:\s*` + returnTypeRe},
		},
	}

	return &Language{
		Name:    "ActionScript",
		Aliases: []string{"as"},
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "as break case catch class const continue default delete do dynamic each else extends final finally " +
					"for function get if implements import in include instanceof interface internal is namespace native " +
					"new override package private protected public return set static super switch this throw try typeof " +
					"use var void while with",
				"literal": "true false null undefined",
			},
			Illegal: `#`,
			Contains: []*Mode{
				AposStringMode,
				QuoteStringMode,
				CLineCommentMode,
				CBlockCommentMode,
				CNumberMode,
				{ClassName: "class", BeginKeywords: "package", End: `\{`, Contains: []*Mode{TitleMode}},
				{
					ClassName:     "class",
					BeginKeywords: "class interface",
					End:           `\{`,
					ExcludeEnd:    true,
					Contains:      []*Mode{{BeginKeywords: "extends implements"}, TitleMode},
				},
				{
					ClassName:     "meta",
					BeginKeywords: "import include",
					End:           `;`,
					Keywords:      Keywords{"meta-keyword": "import include"},
				},
				function,
				MethodGuard,
			},
		},
	}
}
