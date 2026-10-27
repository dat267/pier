package highlight

func init() { RegisterLanguage("brainfuck", Brainfuck()) }

// Brainfuck returns the Brainfuck language definition (a port of
// languages/brainfuck.js). The comment rule treats any run of non-command
// characters as a comment; returnEnd leaves the command that terminates it for
// the next rule.
func Brainfuck() *Language {
	comment := CommentMode(`[^\[\]\.,\+\-<> \r\n]`, `[\[\]\.,\+\-<> \r\n]`)
	comment.ReturnEnd = true
	comment.HasRelevance = true
	comment.Relevance = 0

	literal := &Mode{ClassName: "literal", Begin: `[+-]`, HasRelevance: true, Relevance: 0}

	return &Language{
		Name:    "Brainfuck",
		Aliases: []string{"bf"},
		Contains: []*Mode{
			comment,
			{ClassName: "title", Begin: `[\[\]]`, HasRelevance: true, Relevance: 0},
			{ClassName: "string", Begin: `[\.,]`, HasRelevance: true, Relevance: 0},
			// The only relevance counter in this language.
			{Begin: `(?:\+\+|--)`, Contains: []*Mode{literal}},
			literal,
		},
	}
}
