package highlight

func init() { RegisterLanguage("fix", FIX()) }

// FIX returns the FIX language definition (a port of languages/fix.js). The
// record and field separators are the Unicode symbols U+2401 and U+0001.
func FIX() *Language {
	return &Language{
		Name:            "FIX",
		CaseInsensitive: true,
		Contains: []*Mode{
			{
				Begin:       `[^\u2401\u0001]+`,
				End:         `[\u2401\u0001]`,
				ExcludeEnd:  true,
				ReturnBegin: true,
				Contains: []*Mode{
					{
						ClassName: `attr`,
						Begin:     `([^\u2401\u0001=]+)`,
						End:       `=([^\u2401\u0001=]+)`,
						ReturnEnd: true,
					},
					{
						ClassName:    "string",
						Begin:        `=`,
						End:          `([\u2401\u0001])`,
						ExcludeEnd:   true,
						ExcludeBegin: true,
					},
				},
			},
		},
	}
}
