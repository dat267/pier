package highlight

import "strings"

func init() { RegisterLanguage("julia-repl", JuliaRepl()) }

// JuliaRepl returns the Julia REPL language definition (a port of
// languages/julia-repl.js). The sub-language ends at the first line that is not
// indented by six spaces; the reference writes that as `^(?![ ]{6})`, which is an
// `^` end with an onEnd guard here.
func JuliaRepl() *Language {
	starts := &Mode{End: `^`, SubLanguage: []string{"julia"}}
	starts.OnEnd = func(m *match, resp *Response) {
		if strings.HasPrefix(m.input[m.index+len(m.lexeme):], "      ") {
			resp.IgnoreMatch()
		}
	}
	return &Language{
		Name:    "Julia REPL",
		Aliases: []string{"jldoctest"},
		Contains: []*Mode{
			{ClassName: "meta", Begin: `^julia>`, HasRelevance: true, Relevance: 10, Starts: starts},
		},
	}
}
