package highlight

func init() { RegisterLanguage("leaf", Leaf()) }

// Leaf returns the Leaf language definition (a port of languages/leaf.js).
func Leaf() *Language {
	return &Language{
		Name: "Leaf",
		Contains: []*Mode{
			{
				ClassName:   "function",
				Begin:       `#+[A-Za-z_0-9]*\(`,
				End:         ` \{`,
				ReturnBegin: true,
				ExcludeEnd:  true,
				Contains: []*Mode{
					{ClassName: "keyword", Begin: `#+`},
					{ClassName: "title", Begin: `[A-Za-z_][A-Za-z_0-9]*`},
					{
						ClassName:  "params",
						Begin:      `\(`,
						End:        `\)`,
						EndsParent: true,
						Contains: []*Mode{
							{ClassName: "string", Begin: `"`, End: `"`},
							{ClassName: "variable", Begin: `[A-Za-z_][A-Za-z_0-9]*`},
						},
					},
				},
			},
		},
	}
}
