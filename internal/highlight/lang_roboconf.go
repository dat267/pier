package highlight

func init() { RegisterLanguage("roboconf", Roboconf()) }

// Roboconf returns the Roboconf language definition (a port of
// languages/roboconf.js).
func Roboconf() *Language {
	const identifier = `[a-zA-Z-_][^\n{]+\{`

	property := &Mode{
		ClassName:  "attribute",
		Begin:      `[a-zA-Z-_]+`,
		End:        `\s*:`,
		ExcludeEnd: true,
		Starts: &Mode{
			End:          `;`,
			HasRelevance: true,
			Relevance:    0,
			Contains: []*Mode{
				{ClassName: "variable", Begin: `\.[a-zA-Z-_]+`},
				{ClassName: "keyword", Begin: `\(optional\)`},
			},
		},
	}

	return &Language{
		Name:            "Roboconf",
		Aliases:         []string{"graph", "instances"},
		CaseInsensitive: true,
		Mode: Mode{
			Keywords: Keywords{"keyword": "import"},
			Contains: []*Mode{
				{
					Begin:    `^facet ` + identifier,
					End:      `\}`,
					Keywords: Keywords{"keyword": "facet"},
					Contains: []*Mode{property, HashCommentMode},
				},
				{
					Begin:    `^\s*instance of ` + identifier,
					End:      `\}`,
					Keywords: Keywords{"keyword": "name count channels instance-data instance-state instance of"},
					Illegal:  `\S`,
					Contains: []*Mode{Self, property, HashCommentMode},
				},
				{
					Begin:    `^` + identifier,
					End:      `\}`,
					Contains: []*Mode{property, HashCommentMode},
				},
				HashCommentMode,
			},
		},
	}
}
