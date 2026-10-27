package highlight

func init() { RegisterLanguage("dust", Dust()) }

// Dust returns the Dust language definition (a port of languages/dust.js).
func Dust() *Language {
	return &Language{
		Name:            "Dust",
		Aliases:         []string{"dst"},
		CaseInsensitive: true,
		SubLanguage:     []string{"xml"},
		Contains: []*Mode{
			{
				ClassName: "template-tag",
				Begin:     `\{[#\/]`,
				End:       `\}`,
				Illegal:   `;`,
				Contains: []*Mode{
					{
						ClassName: "name",
						Begin:     `[a-zA-Z\.-]+`,
						Starts: &Mode{
							EndsWithParent: true,
							HasRelevance:   true,
							Relevance:      0,
							Contains:       []*Mode{QuoteStringMode},
						},
					},
				},
			},
			{
				ClassName: "template-variable",
				Begin:     `\{`,
				End:       `\}`,
				Illegal:   `;`,
				Keywords:  Keywords{"keyword": "if eq ne lt lte gt gte select default math sep"},
			},
		},
	}
}
