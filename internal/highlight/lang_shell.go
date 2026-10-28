package highlight

func init() { RegisterLanguage("shell", Shell()) }

// shellEndGuard is the reference's `(?=\s*$)` lookahead on the shell prompt's
// bash sub-language end. The multiline flag makes `$` match a line end inside
// the remainder, not only the end of the whole input.
var shellEndGuard = mustCompileJS(`(?m)\A\s*$`)

// Shell returns the Shell Session language definition (a port of
// languages/shell.js). The end rule's lookahead becomes an onEnd guard, so the
// end lexeme stays the single non-backslash character.
func Shell() *Language {
	starts := &Mode{End: `[^\\]`, SubLanguage: []string{"bash"}}
	starts.OnEnd = func(m *match, resp *Response) {
		if !shellEndGuard.MatchString(m.input[m.index+len(m.lexeme):]) {
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
