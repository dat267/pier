package highlight

func init() { RegisterLanguage("step21", STEP21()) }

// STEP21 returns the STEP Part 21 language definition (a port of
// languages/step21.js). The inherited string modes drop their `illegal`.
func STEP21() *Language {
	apos := Variant(AposStringMode, func(m *Mode) { m.Illegal = "" })
	quote := Variant(QuoteStringMode, func(m *Mode) { m.Illegal = "" })
	symbolBase := &Mode{ClassName: "symbol"}
	symbolBase.Variants = []*Mode{
		Variant(symbolBase, func(m *Mode) { m.Begin = `#`; m.End = `\d+`; m.Illegal = `\W` }),
	}
	return &Language{
		Name:            "STEP Part 21",
		Aliases:         []string{"p21", "step", "stp"},
		CaseInsensitive: true,
		Mode: Mode{
			Keywords: Keywords{
				"$pattern": "[A-Z_][A-Z0-9_.]*",
				"keyword":  "HEADER ENDSEC DATA",
			},
			Contains: []*Mode{
				{ClassName: "meta", Begin: `ISO-10303-21;`, HasRelevance: true, Relevance: 10},
				{ClassName: "meta", Begin: `END-ISO-10303-21;`, HasRelevance: true, Relevance: 10},
				CLineCommentMode,
				CBlockCommentMode,
				CommentMode(`/\*\*!`, `\*/`),
				CNumberMode,
				apos,
				quote,
				{ClassName: "string", Begin: `'`, End: `'`},
				symbolBase,
			},
		},
	}
}
