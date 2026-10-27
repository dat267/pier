package highlight

func init() { RegisterLanguage("monkey", Monkey()) }

// Monkey returns the Monkey language definition (a port of languages/monkey.js).
func Monkey() *Language {
	number := &Mode{ClassName: "number", HasRelevance: true, Relevance: 0}
	number.Variants = []*Mode{
		Variant(number, func(m *Mode) { m.Begin = `[$][a-fA-F0-9]+` }),
		Variant(number, func(m *Mode) { m.Begin = NumberRe }),
	}

	quoteComment := CommentMode(`'`, `$`)
	quoteComment.HasRelevance = true
	quoteComment.Relevance = 0

	return &Language{
		Name:            "Monkey",
		CaseInsensitive: true,
		Keywords: Keywords{
			"keyword": "public private property continue exit extern new try catch eachin not abstract final select case " +
				"default const local global field end if then else elseif endif while wend repeat until forever for " +
				"to step next return module inline throw import",
			"built_in": "DebugLog DebugStop Error Print ACos ACosr ASin ASinr ATan ATan2 ATan2r ATanr Abs Abs Ceil Clamp " +
				"Clamp Cos Cosr Exp Floor Log Max Max Min Min Pow Sgn Sgn Sin Sinr Sqrt Tan Tanr Seed PI HALFPI TWOPI",
			"literal": "true false null and or shl shr mod",
		},
		Illegal: `/\*`,
		Contains: []*Mode{
			CommentMode(`#rem`, `#end`),
			quoteComment,
			{
				ClassName:     "function",
				BeginKeywords: "function method",
				End:           `[(=:]|$`,
				Illegal:       `\n`,
				Contains:      []*Mode{UnderscoreTitleMode},
			},
			{
				ClassName:     "class",
				BeginKeywords: "class interface",
				End:           `$`,
				Contains: []*Mode{
					{BeginKeywords: "extends implements"},
					UnderscoreTitleMode,
				},
			},
			{ClassName: "built_in", Begin: `\b(self|super)\b`},
			{
				ClassName: "meta",
				Begin:     `\s*#`,
				End:       `$`,
				Keywords:  Keywords{"meta-keyword": "if else elseif endif end then"},
			},
			{ClassName: "meta", Begin: `^\s*strict\b`},
			{BeginKeywords: "alias", End: `=`, Contains: []*Mode{UnderscoreTitleMode}},
			QuoteStringMode,
			number,
		},
	}
}
