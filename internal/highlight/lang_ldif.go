package highlight

func init() { RegisterLanguage("ldif", LDIF()) }

// LDIF returns the LDIF language definition (a port of languages/ldif.js).
func LDIF() *Language {
	valueLine := &Mode{End: `$`, HasRelevance: true, Relevance: 0}
	return &Language{
		Name: "LDIF",
		Mode: Mode{
			Contains: []*Mode{
				{
					ClassName:    "attribute",
					Begin:        `^dn`,
					End:          `: `,
					ExcludeEnd:   true,
					Starts:       valueLine,
					HasRelevance: true,
					Relevance:    10,
				},
				{
					ClassName:  "attribute",
					Begin:      `^\w`,
					End:        `: `,
					ExcludeEnd: true,
					Starts:     valueLine,
				},
				{ClassName: "literal", Begin: `^-`, End: `$`},
				HashCommentMode,
			},
		},
	}
}
