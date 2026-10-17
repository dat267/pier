package highlight

func init() { RegisterLanguage("elm", Elm()) }

// Elm returns the Elm language definition (a port of languages/elm.js).
func Elm() *Language {
	commentDash := CommentMode(`--`, `$`)
	commentBlock := CommentMode(`\{-`, `-\}`)
	commentBlock.Contains = append([]*Mode{Self}, commentBlock.Contains...)
	comment := &Mode{Variants: []*Mode{commentDash, commentBlock}}

	constructor := &Mode{ClassName: "type", Begin: `\b[A-Z][\w']*`, HasRelevance: true, Relevance: 0}

	list := &Mode{
		Begin:   `\(`,
		End:     `\)`,
		Illegal: `"`,
		Contains: []*Mode{
			{ClassName: "type", Begin: `\b[A-Z][\w]*(\((?:\.\.|,|\w+)\))?`},
			comment,
		},
	}
	record := &Mode{Begin: `\{`, End: `\}`, Contains: list.Contains}

	character := &Mode{ClassName: "string", Begin: `'\\?.`, End: `'`, Illegal: `.`}

	title := Variant(TitleMode, func(m *Mode) { m.Begin = `^[_a-z][\w']*` })

	return &Language{
		Name: "Elm",
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "let in if then else case of where module import exposing type alias as infix infixl infixr port " +
					"effect command subscription",
			},
			Illegal: `;`,
			Contains: []*Mode{
				{
					BeginKeywords: "port effect module",
					End:           `exposing`,
					Keywords:      Keywords{"keyword": "port effect module where command subscription exposing"},
					Contains:      []*Mode{list, comment},
					Illegal:       `\W\.|;`,
				},
				{
					Begin:    `import`,
					End:      `$`,
					Keywords: Keywords{"keyword": "import as exposing"},
					Contains: []*Mode{list, comment},
					Illegal:  `\W\.|;`,
				},
				{
					Begin:    `type`,
					End:      `$`,
					Keywords: Keywords{"keyword": "type alias"},
					Contains: []*Mode{constructor, list, record, comment},
				},
				{
					BeginKeywords: "infix infixl infixr",
					End:           `$`,
					Contains:      []*Mode{CNumberMode, comment},
				},
				{Begin: `port`, End: `$`, Keywords: Keywords{"keyword": "port"}, Contains: []*Mode{comment}},
				character,
				QuoteStringMode,
				CNumberMode,
				constructor,
				title,
				comment,
				{Begin: `->|<-`},
			},
		},
	}
}
