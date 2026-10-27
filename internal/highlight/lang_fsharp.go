package highlight

func init() { RegisterLanguage("fsharp", FSharp()) }

// FSharp returns the F# language definition (a port of languages/fsharp.js).
func FSharp() *Language {
	typeParam := &Mode{
		Begin: `<`,
		End:   `>`,
		Contains: []*Mode{
			Variant(TitleMode, func(m *Mode) { m.Begin = `'[a-zA-Z0-9_]+` }),
		},
	}
	quote := Variant(QuoteStringMode, func(m *Mode) { m.Illegal = "" })
	return &Language{
		Name:    "F#",
		Aliases: []string{"fs"},
		Keywords: Keywords{
			"keyword": "abstract and as assert base begin class default delegate do done downcast downto elif else end " +
				"exception extern false finally for fun function global if in inherit inline interface internal lazy " +
				"let match member module mutable namespace new null of open or override private public rec return sig " +
				"static struct then to true try type upcast use val void when while with yield",
		},
		Illegal: `/\*`,
		Contains: []*Mode{
			// Match the monad-builder keywords before their plain forms.
			{ClassName: "keyword", Begin: `\b(yield|return|let|do)!`},
			{ClassName: "string", Begin: `@"`, End: `"`, Contains: []*Mode{{Begin: `""`}}},
			{ClassName: "string", Begin: `"""`, End: `"""`},
			func() *Mode {
				m := CommentMode(`\(\*(\s)`, `\*\)`)
				m.Contains = append([]*Mode{Self}, m.Contains...)
				return m
			}(),
			{
				ClassName:     "class",
				BeginKeywords: "type",
				End:           `\(|=|$`,
				ExcludeEnd:    true,
				Contains:      []*Mode{UnderscoreTitleMode, typeParam},
			},
			{ClassName: "meta", Begin: `\[<`, End: `>\]`, HasRelevance: true, Relevance: 10},
			{ClassName: "symbol", Begin: `\B('[A-Za-z])\b`, Contains: []*Mode{BackslashEscape}},
			CLineCommentMode,
			quote,
			CNumberMode,
		},
	}
}
