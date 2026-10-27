package highlight

func init() { RegisterLanguage("subunit", Subunit()) }

// Subunit returns the SubUnit language definition (a port of
// languages/subunit.js).
func Subunit() *Language {
	keywords := &Mode{ClassName: "keyword", HasRelevance: true, Relevance: 10}
	keywords.Variants = []*Mode{
		Variant(keywords, func(m *Mode) {
			m.Begin = `^(test|testing|success|successful|failure|error|skip|xfail|uxsuccess)(:?)\s+(test)?`
		}),
		Variant(keywords, func(m *Mode) { m.Begin = `^progress(:?)(\s+)?(pop|push)?` }),
		Variant(keywords, func(m *Mode) { m.Begin = `^tags:` }),
		Variant(keywords, func(m *Mode) { m.Begin = `^time:` }),
	}
	return &Language{
		Name:            "SubUnit",
		CaseInsensitive: true,
		Contains: []*Mode{
			{ClassName: "string", Begin: `\[\n(multipart)?`, End: `\]\n`},
			{ClassName: "string", Begin: `\d{4}-\d{2}-\d{2}(\s+)\d{2}:\d{2}:\d{2}\.\d+Z`},
			{ClassName: "string", Begin: `(\+|-)\d+`},
			keywords,
		},
	}
}
