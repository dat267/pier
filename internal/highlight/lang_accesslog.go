package highlight

func init() { RegisterLanguage("accesslog", AccessLog()) }

// AccessLog returns the Apache Access Log language definition (a port of
// languages/accesslog.js).
func AccessLog() *Language {
	const verbs = "GET|POST|HEAD|PUT|DELETE|CONNECT|OPTIONS|PATCH|TRACE"
	return &Language{
		Name: "Apache Access Log",
		Mode: Mode{
			Contains: []*Mode{
				{
					ClassName:    "number",
					Begin:        `^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(:\d{1,5})?\b`,
					HasRelevance: true,
					Relevance:    5,
				},
				{ClassName: "number", Begin: `\b\d+\b`, HasRelevance: true, Relevance: 0},
				{
					ClassName:    "string",
					Begin:        `"(` + verbs + `)`,
					End:          `"`,
					Keywords:     Keywords{"keyword": "GET POST HEAD PUT DELETE CONNECT OPTIONS PATCH TRACE"},
					Illegal:      `\n`,
					HasRelevance: true,
					Relevance:    5,
					Contains: []*Mode{
						{Begin: `HTTP\/[12]\.\d'`, HasRelevance: true, Relevance: 5},
					},
				},
				{
					ClassName:    "string",
					Begin:        `\[\d[^\]\n]{8,}\]`,
					Illegal:      `\n`,
					HasRelevance: true,
					Relevance:    1,
				},
				{ClassName: "string", Begin: `\[`, End: `\]`, Illegal: `\n`, HasRelevance: true, Relevance: 0},
				{
					ClassName:    "string",
					Begin:        `"Mozilla\/\d\.\d \(`,
					End:          `"`,
					Illegal:      `\n`,
					HasRelevance: true,
					Relevance:    3,
				},
				{ClassName: "string", Begin: `"`, End: `"`, Illegal: `\n`, HasRelevance: true, Relevance: 0},
			},
		},
	}
}
