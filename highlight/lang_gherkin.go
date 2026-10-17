package highlight

func init() { RegisterLanguage("gherkin", Gherkin()) }

// Gherkin returns the Gherkin language definition (a port of
// languages/gherkin.js). The reference writes the multi-word keywords with
// escaped spaces, which its keyword scanner splits on spaces, so the port lists
// the words individually.
func Gherkin() *Language {
	return &Language{
		Name:    "Gherkin",
		Aliases: []string{"feature"},
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "Feature Background Ability Business Need Scenario Scenarios Scenario " +
					"Outline Scenario Template Examples Given And Then But When",
			},
			Contains: []*Mode{
				{ClassName: "symbol", Begin: `\*`, HasRelevance: true, Relevance: 0},
				{ClassName: "meta", Begin: `@[^@\s]+`},
				{
					Begin:    `\|`,
					End:      `\|\w*$`,
					Contains: []*Mode{{ClassName: "string", Begin: `[^|]+`}},
				},
				{ClassName: "variable", Begin: `<`, End: `>`},
				HashCommentMode,
				{ClassName: "string", Begin: `"""`, End: `"""`},
				QuoteStringMode,
			},
		},
	}
}
