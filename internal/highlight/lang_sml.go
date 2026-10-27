package highlight

func init() { RegisterLanguage("sml", SML()) }

// SML returns the SML (Standard ML) language definition (a port of
// languages/sml.js).
func SML() *Language {
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
		Name:    "SML (Standard ML)",
		Aliases: []string{"ml"},
		Keywords: Keywords{
			"$pattern": "[a-z_]\\w*!?",
			"keyword": "abstype and andalso as case datatype do else end eqtype exception fn fun functor handle if in " +
				"include infix infixr let local nonfix of op open orelse raise rec sharing sig signature struct " +
				"structure then type val with withtype where while",
			"built_in": "array bool char exn int list option order real ref string substring vector unit word",
			"literal":  "true false NONE SOME LESS EQUAL GREATER nil",
		},
		Illegal: `\/\/|>>`,
		Contains: []*Mode{
			{ClassName: "literal", Begin: `\[(\|\|)?\]|\(\)`, HasRelevance: true, Relevance: 0},
			comment,
			typeVariable,
			{ClassName: "type", Begin: "\x60[A-Z][\\w']*"},
			{ClassName: "type", Begin: `\b[A-Z][\w']*`, HasRelevance: true, Relevance: 0},
			{Begin: `[a-z_]\w*'[\w']*`},
			apos,
			quote,
			{
				ClassName:    "number",
				Begin:        `\b(0[xX][a-fA-F0-9_]+[Lln]?|0[oO][0-7_]+[Lln]?|0[bB][01_]+[Lln]?|[0-9][0-9_]*([Lln]|(\.[0-9_]*)?([eE][-+]?[0-9_]+)?)?)`,
				HasRelevance: true,
				Relevance:    0,
			},
			{Begin: `[-=]>`},
		},
	}
}
