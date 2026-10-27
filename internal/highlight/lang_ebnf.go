package highlight

func init() { RegisterLanguage("ebnf", EBNF()) }

// EBNF returns the Extended Backus-Naur Form language definition (a port of
// languages/ebnf.js). The terminal variants inherit the string class from their
// base through Variant.
func EBNF() *Language {
	comment := CommentMode(`\(\*`, `\*\)`)

	nonTerminal := &Mode{ClassName: "attribute", Begin: `^[ ]*[a-zA-Z]+([\s_-]+[a-zA-Z]+)*`}
	specialSequence := &Mode{ClassName: "meta", Begin: `\?.*\?`}

	terminal := &Mode{ClassName: "string"}
	terminal.Variants = []*Mode{
		Variant(terminal, func(m *Mode) {
			m.Begin = `'`
			m.End = `'`
			m.Illegal = `\n`
			m.Contains = []*Mode{BackslashEscape}
		}),
		Variant(terminal, func(m *Mode) {
			m.Begin = `"`
			m.End = `"`
			m.Illegal = `\n`
			m.Contains = []*Mode{BackslashEscape}
		}),
		Variant(terminal, func(m *Mode) { m.Begin = "\x60"; m.End = "\x60" }),
	}

	ruleBody := &Mode{
		Begin:    `=`,
		End:      `[.;]`,
		Contains: []*Mode{comment, specialSequence, terminal},
	}

	return &Language{
		Name:     "Extended Backus-Naur Form",
		Illegal:  `\S`,
		Contains: []*Mode{comment, nonTerminal, ruleBody},
	}
}
