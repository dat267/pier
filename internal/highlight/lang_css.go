package highlight

import "strings"

func init() { RegisterLanguage("css", CSS()) }

// cssAttributesAlternation is ATTRIBUTES.reverse().join("|") from css.js. The
// reverse makes longer property names win the leftmost-first alternation, so
// `background-color` is not shortened to `background`.
var cssAttributesAlternation = func() string {
	reversed := make([]string, len(cssAttributes))
	for i, attr := range cssAttributes {
		reversed[len(cssAttributes)-1-i] = attr
	}
	return strings.Join(reversed, "|")
}()

// CSS returns the CSS language definition (a port of languages/css.js).
func CSS() *Language {
	identRe := `[a-zA-Z-][a-zA-Z0-9_-]*`

	functionDispatch := &Mode{ClassName: "built_in", Begin: `[\w-]+`}
	functionDispatch.beforeBegin = func(m *match, resp *Response) {
		next := m.index + len(m.lexeme)
		if next >= len(m.input) || m.input[next] != '(' {
			resp.IgnoreMatch()
		}
	}
	vendorPrefix := &Mode{Begin: `-(webkit|moz|ms|o)-`}
	vendorPrefix.beforeBegin = func(m *match, resp *Response) {
		next := m.index + len(m.lexeme)
		if next >= len(m.input) || m.input[next] < 'a' || m.input[next] > 'z' {
			resp.IgnoreMatch()
		}
	}

	attributeSelector := &Mode{
		ClassName: "selector-attr",
		Begin:     `\[`,
		End:       `\]`,
		Illegal:   `$`,
		Contains:  []*Mode{AposStringMode, QuoteStringMode},
	}
	hexColor := &Mode{ClassName: "number", Begin: `#([a-fA-F0-9]{6}|[a-fA-F0-9]{3})`}
	important := &Mode{ClassName: "meta", Begin: `!important`}

	urlMode := Zero(Mode{
		Begin:    `(url|data-uri)\(`,
		End:      `\)`,
		Keywords: Keywords{"built_in": "url data-uri"},
		Contains: []*Mode{
			{ClassName: "string", Begin: `[^)]`, EndsWithParent: true, ExcludeEnd: true},
		},
	})

	attributeValue := &Mode{
		Begin: `:`,
		End:   `[;}]`,
		Contains: []*Mode{
			hexColor,
			important,
			CSSNumberMode,
			AposStringMode,
			QuoteStringMode,
			urlMode,
			functionDispatch,
		},
	}

	mediaAttribute := &Mode{ClassName: "attribute", Begin: `[a-z-]+`}
	mediaAttribute.beforeBegin = func(m *match, resp *Response) {
		next := m.index + len(m.lexeme)
		if next >= len(m.input) || m.input[next] != ':' {
			resp.IgnoreMatch()
		}
	}

	// The reference begins the at-rule mode with `(?=@)` (zero-width) and then
	// matches @property as a keyword inside; consuming `@` and returning before
	// it keeps the same span.
	atRule := &Mode{
		Begin:        `@`,
		ReturnBegin:  true,
		End:          `[{;]`,
		HasRelevance: true,
		Relevance:    0,
		Illegal:      `:`,
		Contains: []*Mode{
			{ClassName: "keyword", Begin: `@-?\w[\w]*(-\w+)*`},
			{
				Begin:          `\s`,
				EndsWithParent: true,
				ExcludeEnd:     true,
				HasRelevance:   true,
				Relevance:      0,
				Keywords: Keywords{
					"$pattern":  `[a-z-]+`,
					"keyword":   "and or not only",
					"attribute": strings.Join(cssMediaFeatures, " "),
				},
				Contains: []*Mode{
					mediaAttribute,
					AposStringMode,
					QuoteStringMode,
					CSSNumberMode,
				},
			},
		},
	}

	return &Language{
		Name:             "CSS",
		CaseInsensitive:  true,
		ClassNameAliases: map[string]string{"keyframePosition": "selector-tag"},
		Mode: Mode{
			Illegal:  `[=|'\$]`,
			Keywords: Keywords{"keyframePosition": "from to"},
			Contains: []*Mode{
				CBlockCommentMode,
				vendorPrefix,
				CSSNumberMode,
				Zero(Mode{ClassName: "selector-id", Begin: `#[A-Za-z0-9_-]+`}),
				Zero(Mode{ClassName: "selector-class", Begin: `\.` + identRe}),
				attributeSelector,
				{
					ClassName: "selector-pseudo",
					Variants: []*Mode{
						{ClassName: "selector-pseudo", Begin: `:(` + strings.Join(cssPseudoClasses, "|") + `)`},
						{ClassName: "selector-pseudo", Begin: `::(` + strings.Join(cssPseudoElements, "|") + `)`},
					},
				},
				{ClassName: "attribute", Begin: `\b(` + cssAttributesAlternation + `)\b`},
				attributeValue,
				atRule,
				{ClassName: "selector-tag", Begin: `\b(` + strings.Join(cssTags, "|") + `)\b`},
			},
		},
	}
}
