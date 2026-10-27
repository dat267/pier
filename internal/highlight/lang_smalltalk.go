package highlight

func init() { RegisterLanguage("smalltalk", Smalltalk()) }

// smalltalkVarIdentRe matches a Smalltalk variable identifier.
const smalltalkVarIdentRe = `[a-z][a-zA-Z0-9_]*`

// Smalltalk returns the Smalltalk language definition (a port of
// languages/smalltalk.js).
func Smalltalk() *Language {
	return &Language{
		Name:     "Smalltalk",
		Aliases:  []string{"st"},
		Keywords: Keywords{"keyword": "self super nil true false thisContext"},
		Contains: []*Mode{
			CommentMode(`"`, `"`),
			AposStringMode,
			{ClassName: "type", Begin: `\b[A-Z][A-Za-z0-9_]*`, HasRelevance: true, Relevance: 0},
			{Begin: smalltalkVarIdentRe + `:`, HasRelevance: true, Relevance: 0},
			CNumberMode,
			{ClassName: "symbol", Begin: `#` + UnderscoreIdentRe},
			{ClassName: "string", Begin: `\$.{1}`},
			{
				// Effectively `| var1 var2 ... |`, whitespace adjacent to the
				// bars optional.
				Begin:       `\|[ ]*` + smalltalkVarIdentRe + `([ ]+` + smalltalkVarIdentRe + `)*[ ]*\|`,
				ReturnBegin: true,
				End:         `\|`,
				Illegal:     `\S`,
				Contains:    []*Mode{{Begin: `(\|[ ]*)?` + smalltalkVarIdentRe}},
			},
			{
				Begin: `#\(`,
				End:   `\)`,
				Contains: []*Mode{
					AposStringMode,
					{ClassName: "string", Begin: `\$.{1}`},
					CNumberMode,
					{ClassName: "symbol", Begin: `#` + UnderscoreIdentRe},
				},
			},
		},
	}
}
