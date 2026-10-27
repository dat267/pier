package highlight

func init() { RegisterLanguage("bnf", BNF()) }

// BNF returns the Backus-Naur Form language definition (a port of
// languages/bnf.js).
func BNF() *Language {
	attribute := &Mode{ClassName: "attribute", Begin: `<`, End: `>`}
	return &Language{
		Name: "Backus–Naur Form",
		Contains: []*Mode{
			attribute,
			{
				Begin: `::=`,
				End:   `$`,
				Contains: []*Mode{
					{Begin: `<`, End: `>`},
					CLineCommentMode,
					CBlockCommentMode,
					AposStringMode,
					QuoteStringMode,
				},
			},
		},
	}
}
