package highlight

func init() { RegisterLanguage("profile", Profile()) }

// Profile returns the Python profiler language definition (a port of
// languages/profile.js).
func Profile() *Language {
	return &Language{
		Name: "Python profiler",
		Contains: []*Mode{
			CNumberMode,
			{Begin: `[a-zA-Z_][\da-zA-Z_]+\.[\da-zA-Z_]{1,3}`, End: `:`, ExcludeEnd: true},
			{
				Begin:        `(ncalls|tottime|cumtime)`,
				End:          `$`,
				Keywords:     Keywords{"keyword": "ncalls tottime|10 cumtime|10 filename"},
				HasRelevance: true,
				Relevance:    10,
			},
			{
				Begin:        `function calls`,
				End:          `$`,
				Contains:     []*Mode{CNumberMode},
				HasRelevance: true,
				Relevance:    10,
			},
			AposStringMode,
			QuoteStringMode,
			{
				ClassName:    "string",
				Begin:        `\(`,
				End:          `\)$`,
				ExcludeBegin: true,
				ExcludeEnd:   true,
				HasRelevance: true,
				Relevance:    0,
			},
		},
	}
}
