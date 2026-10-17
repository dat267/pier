package highlight

// replPromptLookahead implements the reference's `(?=[ ]|$)` lookahead after a
// REPL prompt: the next byte must be a space, a newline, or end of input.
func replPromptLookahead(m *match) bool {
	pos := m.index + len(m.lexeme)
	if pos >= len(m.input) {
		return true
	}
	c := m.input[pos]
	return c == ' ' || c == '\n'
}

func init() {
	RegisterLanguage("python-repl", PythonRepl())
	RegisterLanguage("node-repl", NodeRepl())
}

// PythonRepl returns the Python REPL language definition (a port of
// languages/python-repl.js).
func PythonRepl() *Language {
	inner := &Mode{End: `$`, SubLanguage: []string{"python"}}
	starts := &Mode{End: ` |$`, Starts: inner}
	base := &Mode{ClassName: "meta", Starts: starts}
	base.Variants = []*Mode{
		Variant(base, func(m *Mode) { m.Begin = `^>>>` }),
		Variant(base, func(m *Mode) { m.Begin = `^\.\.\.` }),
	}
	for _, v := range base.Variants {
		v.beforeBegin = func(m *match, resp *Response) {
			if !replPromptLookahead(m) {
				resp.IgnoreMatch()
			}
		}
	}
	return &Language{
		Aliases: []string{"pycon"},
		Mode:    Mode{Contains: []*Mode{base}},
	}
}

// NodeRepl returns the Node REPL language definition (a port of
// languages/node-repl.js).
func NodeRepl() *Language {
	inner := &Mode{End: `$`, SubLanguage: []string{"javascript"}}
	starts := &Mode{End: ` |$`, Starts: inner}
	base := &Mode{ClassName: "meta", Starts: starts}
	base.Variants = []*Mode{
		Variant(base, func(m *Mode) { m.Begin = `^>` }),
		Variant(base, func(m *Mode) { m.Begin = `^\.\.\.` }),
	}
	for _, v := range base.Variants {
		v.beforeBegin = func(m *match, resp *Response) {
			if !replPromptLookahead(m) {
				resp.IgnoreMatch()
			}
		}
	}
	return &Language{
		Name: "Node REPL",
		Mode: Mode{Contains: []*Mode{base}},
	}
}
