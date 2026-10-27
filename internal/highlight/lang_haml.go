package highlight

func init() { RegisterLanguage("haml", Haml()) }

// Haml returns the HAML language definition (a port of languages/haml.js).
func Haml() *Language {
	comment := CommentMode(`^\s*(!=#|=#|-#|/).*$`, `\B|\b`)
	comment.HasRelevance = true
	comment.Relevance = 0

	// `^\s*(-|=|!=)(?!#)`: the `(?!#)` lookahead becomes a beforeBegin guard.
	rubyLine := &Mode{Begin: `^\s*(-|=|!=)`, Starts: &Mode{End: `\n`, SubLanguage: []string{"ruby"}}}
	rubyLine.beforeBegin = func(m *match, resp *Response) {
		if end := m.index + len(m.lexeme); end < len(m.input) && m.input[end] == '#' {
			resp.IgnoreMatch()
		}
	}

	attrValue := func(begin, end string) *Mode {
		return &Mode{
			Begin: begin,
			End:   end,
			Contains: []*Mode{
				{ClassName: "attr", Begin: `:\w+`},
				AposStringMode,
				QuoteStringMode,
				{Begin: `\w+`, HasRelevance: true, Relevance: 0},
			},
		}
	}
	_ = attrValue

	tag := &Mode{
		ClassName: "tag",
		Begin:     `^\s*%`,
		Contains: []*Mode{
			{ClassName: "selector-tag", Begin: `\w+`},
			{ClassName: "selector-id", Begin: `#[\w-]+`},
			{ClassName: "selector-class", Begin: `\.[\w-]+`},
			{
				Begin: `\{\s*`,
				End:   `\s*\}`,
				Contains: []*Mode{
					{
						Begin:          `:\w+\s*=>`,
						End:            `,\s+`,
						ReturnBegin:    true,
						EndsWithParent: true,
						Contains: []*Mode{
							{ClassName: "attr", Begin: `:\w+`},
							AposStringMode,
							QuoteStringMode,
							{Begin: `\w+`, HasRelevance: true, Relevance: 0},
						},
					},
				},
			},
			{
				Begin:          `\(\s*`,
				End:            `\s*\)`,
				ExcludeEnd:     true,
				EndsWithParent: false,
				Contains: []*Mode{
					{
						Begin:          `\w+\s*=`,
						End:            `\s+`,
						ReturnBegin:    true,
						EndsWithParent: true,
						Contains: []*Mode{
							{ClassName: "attr", Begin: `\w+`, HasRelevance: true, Relevance: 0},
							AposStringMode,
							QuoteStringMode,
							{Begin: `\w+`, HasRelevance: true, Relevance: 0},
						},
					},
				},
			},
		},
	}

	return &Language{
		Name:            "HAML",
		CaseInsensitive: true,
		Contains: []*Mode{
			{
				ClassName:    "meta",
				Begin:        `^!!!( (5|1\.1|Strict|Frameset|Basic|Mobile|RDFa|XML\b.*))?$`,
				HasRelevance: true,
				Relevance:    10,
			},
			comment,
			rubyLine,
			tag,
			{Begin: `^\s*[=~]\s*`},
			{Begin: `#\{`, Starts: &Mode{End: `\}`, SubLanguage: []string{"ruby"}}},
		},
	}
}
