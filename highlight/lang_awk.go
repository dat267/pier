package highlight

func init() { RegisterLanguage("awk", Awk()) }

// Awk returns the Awk language definition (a port of languages/awk.js).
func Awk() *Language {
	variable := &Mode{ClassName: "variable"}
	variable.Variants = []*Mode{
		Variant(variable, func(m *Mode) { m.Begin = `\$[\w\d#@][\w\d_]*` }),
		Variant(variable, func(m *Mode) { m.Begin = `\$\{(.*?)\}` }),
	}

	str := &Mode{ClassName: "string", Contains: []*Mode{BackslashEscape}}
	str.Variants = []*Mode{
		Variant(str, func(m *Mode) {
			m.Begin = `(u|b)?r?'''`
			m.End = `'''`
			m.HasRelevance = true
			m.Relevance = 10
		}),
		Variant(str, func(m *Mode) {
			m.Begin = `(u|b)?r?"""`
			m.End = `"""`
			m.HasRelevance = true
			m.Relevance = 10
		}),
		Variant(str, func(m *Mode) { m.Begin = `(u|r|ur)'`; m.End = `'`; m.HasRelevance = true; m.Relevance = 10 }),
		Variant(str, func(m *Mode) { m.Begin = `(u|r|ur)"`; m.End = `"`; m.HasRelevance = true; m.Relevance = 10 }),
		Variant(str, func(m *Mode) { m.Begin = `(b|br)'`; m.End = `'` }),
		Variant(str, func(m *Mode) { m.Begin = `(b|br)"`; m.End = `"` }),
		Variant(str, func(m *Mode) { m.Begin = `'`; m.End = `'`; m.Illegal = `\n` }),
		Variant(str, func(m *Mode) { m.Begin = `"`; m.End = `"`; m.Illegal = `\n` }),
	}

	return &Language{
		Name: "Awk",
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "BEGIN END if else while do for in break continue delete next nextfile function func exit|10",
			},
			Contains: []*Mode{
				variable,
				str,
				RegexpMode,
				HashCommentMode,
				NumberMode,
			},
		},
	}
}
