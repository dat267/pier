package highlight

func init() { RegisterLanguage("xml", XML()) }

func isWhitespaceByte(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}

// XML returns the HTML/XML language definition (a port of languages/xml.js).
func XML() *Language {
	const tagNameRe = `[A-Z_]([A-Z0-9_.-]*:)?[A-Z0-9_.-]*`
	const xmlIdentRe = `[A-Za-z0-9._:-]+`

	openTagGuard := mustCompileJS(`(?mi)\A(?:` + tagNameRe + `)(?:/>|>|\s)`)
	closeTagGuard := mustCompileJS(`(?mi)\A(?:` + tagNameRe + `)>`)

	entities := &Mode{ClassName: "symbol", Begin: `&[a-z]+;|&#[0-9]+;|&#x[a-f0-9]+;`}
	metaKeywords := &Mode{
		Begin: `\s`,
		Contains: []*Mode{
			{ClassName: "meta-keyword", Begin: `#?[a-z_][a-z1-9_-]+`, Illegal: `\n`},
		},
	}
	metaParKeywords := Variant(metaKeywords, func(m *Mode) { m.Begin = `\(`; m.End = `\)` })
	aposMetaString := Variant(AposStringMode, func(m *Mode) { m.ClassName = "meta-string" })
	quoteMetaString := Variant(QuoteStringMode, func(m *Mode) { m.ClassName = "meta-string" })

	tagInternals := &Mode{
		EndsWithParent: true,
		Illegal:        `<`,
		HasRelevance:   true,
		Relevance:      0,
		Contains: []*Mode{
			Zero(Mode{ClassName: "attr", Begin: xmlIdentRe}),
			{
				Begin: `=\s*`,
				Contains: []*Mode{
					{
						ClassName:  "string",
						EndsParent: true,
						Variants: []*Mode{
							{ClassName: "string", Begin: `"`, End: `"`, Contains: []*Mode{entities}},
							{ClassName: "string", Begin: `'`, End: `'`, Contains: []*Mode{entities}},
							{ClassName: "string", Begin: "[^\\s\"'=<>`]+"},
						},
					},
				},
			},
		},
	}

	meta := &Mode{
		ClassName:    "meta",
		Begin:        `<![a-z]`,
		End:          `>`,
		HasRelevance: true,
		Relevance:    10,
		Contains: []*Mode{
			metaKeywords,
			quoteMetaString,
			aposMetaString,
			metaParKeywords,
			{
				Begin: `\[`,
				End:   `\]`,
				Contains: []*Mode{
					{
						ClassName: "meta",
						Begin:     `<![a-z]`,
						End:       `>`,
						Contains: []*Mode{
							metaKeywords,
							metaParKeywords,
							quoteMetaString,
							aposMetaString,
						},
					},
				},
			},
		},
	}

	commentRule := commentMode(`<!--`, `-->`)
	commentRule.HasRelevance = true
	commentRule.Relevance = 10

	cdata := &Mode{Begin: `<!\[CDATA\[`, End: `\]\]>`, HasRelevance: true, Relevance: 10}
	processingInstruction := &Mode{ClassName: "meta", Begin: `<\?xml`, End: `\?>`, HasRelevance: true, Relevance: 10}

	styleTag := &Mode{
		ClassName: "tag",
		Begin:     `<style`,
		End:       `>`,
		Keywords:  Keywords{"name": "style"},
		Contains:  []*Mode{tagInternals},
		Starts: &Mode{
			End:         `</style>`,
			ReturnEnd:   true,
			SubLanguage: []string{"css", "xml"},
		},
	}
	styleTag.beforeBegin = func(m *match, resp *Response) {
		next := m.index + len(m.lexeme)
		if next < len(m.input) && m.input[next] != '>' && !isWhitespaceByte(m.input[next]) {
			resp.IgnoreMatch()
		}
	}
	scriptTag := &Mode{
		ClassName: "tag",
		Begin:     `<script`,
		End:       `>`,
		Keywords:  Keywords{"name": "script"},
		Contains:  []*Mode{tagInternals},
		Starts: &Mode{
			End:         `</script>`,
			ReturnEnd:   true,
			SubLanguage: []string{"javascript", "handlebars", "xml"},
		},
	}
	scriptTag.beforeBegin = func(m *match, resp *Response) {
		next := m.index + len(m.lexeme)
		if next < len(m.input) && m.input[next] != '>' && !isWhitespaceByte(m.input[next]) {
			resp.IgnoreMatch()
		}
	}

	fragmentTag := &Mode{ClassName: "tag", Begin: `<>|</>`}
	openTag := &Mode{ClassName: "tag", Begin: `<`, End: `/?>`}
	openTag.beforeBegin = func(m *match, resp *Response) {
		next := m.index + len(m.lexeme)
		if next >= len(m.input) || !openTagGuard.MatchString(m.input[next:]) {
			resp.IgnoreMatch()
		}
	}
	openTag.Contains = []*Mode{
		Zero(Mode{ClassName: "name", Begin: tagNameRe, Starts: tagInternals}),
	}
	closeTag := &Mode{
		ClassName: "tag",
		Begin:     `</`,
		Contains: []*Mode{
			Zero(Mode{ClassName: "name", Begin: tagNameRe}),
			Zero(Mode{Begin: `>`, EndsParent: true}),
		},
	}
	closeTag.beforeBegin = func(m *match, resp *Response) {
		next := m.index + len(m.lexeme)
		if next >= len(m.input) || !closeTagGuard.MatchString(m.input[next:]) {
			resp.IgnoreMatch()
		}
	}

	return &Language{
		Name: "HTML, XML",
		Aliases: []string{
			"html", "xhtml", "rss", "atom", "xjb", "xsd", "xsl", "plist", "wsf", "svg",
		},
		CaseInsensitive: true,
		Contains: []*Mode{
			meta,
			commentRule,
			cdata,
			entities,
			processingInstruction,
			styleTag,
			scriptTag,
			fragmentTag,
			openTag,
			closeTag,
		},
	}
}
