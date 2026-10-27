package highlight

func init() { RegisterLanguage("erb", ERB()) }

// ERB returns the ERB language definition (a port of languages/erb.js). The
// root runs an XML sub-language; its own rules run Ruby.
func ERB() *Language {
	return &Language{
		Name:        "ERB",
		SubLanguage: []string{"xml"},
		Contains: []*Mode{
			CommentMode(`<%#`, `%>`),
			{
				Begin:        `<%[%=-]?`,
				End:          `[%-]?%>`,
				SubLanguage:  []string{"ruby"},
				ExcludeBegin: true,
				ExcludeEnd:   true,
			},
		},
	}
}
