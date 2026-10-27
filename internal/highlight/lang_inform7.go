package highlight

func init() { RegisterLanguage("inform7", Inform7()) }

// Inform7 returns the Inform 7 language definition (a port of
// languages/inform7.js).
func Inform7() *Language {
	subst := &Mode{ClassName: "subst", Begin: `\[`, End: `\]`}
	return &Language{
		Name:            "Inform 7",
		Aliases:         []string{"i7"},
		CaseInsensitive: true,
		Keywords: Keywords{
			"keyword": "thing room person man woman animal container supporter backdrop door scenery open closed locked " +
				"inside gender is are say understand kind of rule",
		},
		Contains: []*Mode{
			{
				ClassName:    "string",
				Begin:        `"`,
				End:          `"`,
				HasRelevance: true,
				Relevance:    0,
				Contains:     []*Mode{subst},
			},
			{ClassName: "section", Begin: `^(Volume|Book|Part|Chapter|Section|Table)\b`, End: `$`},
			{
				Begin:    `^(Check|Carry out|Report|Instead of|To|Rule|When|Before|After)\b`,
				End:      `:`,
				Contains: []*Mode{{Begin: `\(This`, End: `\)`}},
			},
			{ClassName: "comment", Begin: `\[`, End: `\]`, Contains: []*Mode{Self}},
		},
	}
}
