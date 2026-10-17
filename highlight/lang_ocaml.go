package highlight

func init() { RegisterLanguage("ocaml", OCaml()) }

// OCaml returns the OCaml language definition (a port of languages/ocaml.js).
func OCaml() *Language {
	typeVariable := &Mode{ClassName: "symbol", Begin: `'[A-Za-z_][\w']*`}
	typeVariable.beforeBegin = func(m *match, resp *Response) {
		if pos := m.index + 2; pos < len(m.input) && m.input[pos] == '\'' {
			resp.IgnoreMatch()
		}
	}

	comment := CommentMode(`\(\*`, `\*\)`)
	comment.Contains = append([]*Mode{Self}, comment.Contains...)

	apos := Variant(AposStringMode, func(m *Mode) { m.ClassName = "string"; m.HasRelevance = true; m.Relevance = 0 })
	quote := Variant(QuoteStringMode, func(m *Mode) { m.Illegal = "" })

	return &Language{
		Name:    "OCaml",
		Aliases: []string{"ml"},
		Mode: Mode{
			Keywords: Keywords{
				"$pattern": "[a-z_]\\w*!?",
				"keyword": "and as assert asr begin class constraint do done downto else end exception external for fun function " +
					"functor if in include inherit! inherit initializer land lazy let lor lsl lsr lxor match method!|10 " +
					"method mod module mutable new object of open! open or private rec sig struct then to try type val! " +
					"val virtual when while with parser value",
				"built_in": "array bool bytes char exn|5 float int int32 int64 list lazy_t|5 nativeint|5 string unit in_channel " +
					"out_channel ref",
				"literal": "true false",
			},
			Illegal: `\/\/|>>`,
			Contains: []*Mode{
				{ClassName: "literal", Begin: `\[(\|\|)?\]|\(\)`, HasRelevance: true, Relevance: 0},
				comment,
				typeVariable,
				{ClassName: "type", Begin: "\x60[A-Z][\\w']*"},
				{ClassName: "type", Begin: `\b[A-Z][\w']*`, HasRelevance: true, Relevance: 0},
				{Begin: `[a-z_]\w*'[\w']*`, HasRelevance: true, Relevance: 0},
				apos,
				quote,
				{
					ClassName:    "number",
					Begin:        `\b(0[xX][a-fA-F0-9_]+[Lln]?|0[oO][0-7_]+[Lln]?|0[bB][01_]+[Lln]?|[0-9][0-9_]*([Lln]|(\.[0-9_]*)?([eE][-+]?[0-9_]+)?)?)`,
					HasRelevance: true,
					Relevance:    0,
				},
				{Begin: `->`},
			},
		},
	}
}
