package highlight

func init() { RegisterLanguage("shell", Shell()) }

// Shell returns the Shell Session language definition (a port of
// languages/shell.js). The end rule's lookahead becomes an onEnd guard, so the
// end lexeme stays the single non-backslash character.
func Shell() *Language {
	starts := &Mode{End: `[^\\]`, SubLanguage: []string{"bash"}}
	// The reference's `(?=\s*$)` lookahead on the bash sub-language end: the
	// m flag makes `$` a line end inside the remainder (spaceToLineEnd).
	starts.OnEnd = func(m *match, resp *Response) {
		if !spaceToLineEnd(m.input[m.index+len(m.lexeme):], m.jsLines) {
			resp.IgnoreMatch()
		}
	}
	return &Language{
		Name:    "Shell Session",
		Aliases: []string{"console"},
		Contains: []*Mode{
			{ClassName: "meta", Begin: `^\s{0,3}[/~\w\d[\]()@-]*[>%$#]`, Starts: starts},
		},
	}
}
