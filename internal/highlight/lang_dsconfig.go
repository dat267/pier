package highlight

func init() { RegisterLanguage("dsconfig", DSConfig()) }

// DSConfig returns the dsconfig language definition (a port of
// languages/dsconfig.js). The valueless-property rule ends on a `(?=\W)`
// lookahead; since its begin already consumes every word character, the mode
// always ends immediately after it, so no end is needed.
func DSConfig() *Language {
	return &Language{
		Keywords: Keywords{
			"keyword": "dsconfig",
		},
		Contains: []*Mode{
			{ClassName: "keyword", Begin: `^dsconfig`, End: `\s`, ExcludeEnd: true, HasRelevance: true, Relevance: 10},
			{
				ClassName:    "built_in",
				Begin:        `(list|create|get|set|delete)-(\w+)`,
				End:          `\s`,
				ExcludeEnd:   true,
				Illegal:      `!@#$%^&*()`,
				HasRelevance: true,
				Relevance:    10,
			},
			{ClassName: "built_in", Begin: `--(\w+)`, End: `\s`, ExcludeEnd: true},
			{ClassName: "string", Begin: `"`, End: `"`},
			{ClassName: "string", Begin: `'`, End: `'`},
			{ClassName: "string", Begin: `[\w\-?]+:\w+`, End: `\W`, HasRelevance: true, Relevance: 0},
			{ClassName: "string", Begin: `\w+(\-\w+)*`, HasRelevance: true, Relevance: 0},
			HashCommentMode,
		},
	}
}
