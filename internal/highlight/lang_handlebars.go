package highlight

import (
	"strings"
)

func init() {
	RegisterLanguage("handlebars", handlebarsLanguage("Handlebars", false))
	RegisterLanguage("htmlbars", handlebarsLanguage("HTMLbars", true))
}

var (
	handlebarsDoubleBrace = mustCompileJS(`\A\{\{`)
	handlebarsElse        = mustCompileJS(`\Aelse\}\}`)
)

// handlebarsLanguage builds the Handlebars definition. htmlbars is the same
// definition with a different name and autodetection disabled; both are built
// fresh because RegisterLanguage records the name on the language.
func handlebarsLanguage(name string, disableAutodetect bool) *Language {
	builtIns := Keywords{"builtin-name": "action bindattr collection component concat debugger each each-in " +
		"get hash if in input link-to loc log lookup mut outlet partial query-params render template " +
		"textarea unbound unless view with yield"}
	literals := Keywords{"literal": "true false undefined null"}

	helperName := &Mode{Begin: "(\\.|\\.\\/|\\/)?(\"\"|\"[^\"]+\"|''|'[^']+'|\\[\\]|\\[[^\\]]+\\]|[^\\s!\"#%&'()*+,.\\/;<=>@\\[\\\\\\]^`{|}~]+)((\\.|\\/)(\"\"|\"[^\"]+\"|''|'[^']+'|\\[\\]|\\[[^\\]]+\\]|[^\\s!\"#%&'()*+,.\\/;<=>@\\[\\\\\\]^`{|}~]+))*", Lexemes: `[\w.\/]+`}
	helperParameter := Variant(helperName, func(m *Mode) { m.Keywords = literals })

	subExpression := &Mode{Begin: `\(`, End: `\)`}

	blockParams := &Mode{
		Begin:    `as\s+\|`,
		Keywords: Keywords{"keyword": "as"},
		End:      `\|`,
		Contains: []*Mode{{Begin: `\w+`}},
	}

	hash := &Mode{ClassName: "attr", Begin: "(\\[\\]|\\[[^\\]]+\\]|[^\\s!\"#%&'()*+,.\\/;<=>@\\[\\\\\\]^`{|}~]+)", HasRelevance: true, Relevance: 0}
	hash.beforeBegin = func(m *match, resp *Response) {
		if !strings.HasPrefix(m.input[m.index+len(m.lexeme):], "=") {
			resp.IgnoreMatch()
		}
	}
	hash.Starts = &Mode{
		Begin: `=`,
		End:   `=`,
		Starts: &Mode{Contains: []*Mode{
			CNumberMode, QuoteStringMode, AposStringMode, helperParameter, subExpression,
		}},
	}

	helperParameters := &Mode{
		Contains: []*Mode{
			CNumberMode, QuoteStringMode, AposStringMode, blockParams, hash, helperParameter, subExpression,
		},
		ReturnEnd: true,
	}

	subExprStart := Variant(helperParameters, func(m *Mode) { m.End = `\)` })
	subExprContents := Variant(helperName, func(m *Mode) {
		m.ClassName = "name"
		m.Keywords = builtIns
		m.Starts = subExprStart
	})
	subExpression.Contains = []*Mode{subExprContents}

	openStart := Variant(helperParameters, func(m *Mode) { m.End = `\}\}` })
	openingBlock := Variant(helperName, func(m *Mode) {
		m.Keywords = builtIns
		m.ClassName = "name"
		m.Starts = openStart
	})
	closingBlock := Variant(helperName, func(m *Mode) { m.Keywords = builtIns; m.ClassName = "name" })
	basicStart := Variant(helperParameters, func(m *Mode) { m.End = `\}\}` })
	basicMustache := Variant(helperName, func(m *Mode) {
		m.ClassName = "name"
		m.Keywords = builtIns
		m.Starts = basicStart
	})

	escape := &Mode{Begin: `\\\{\{`, Skip: true}
	preventEscape := &Mode{Begin: `\\\\`, Skip: true}
	preventEscape.beforeBegin = func(m *match, resp *Response) {
		if !startsWith(handlebarsDoubleBrace, m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}

	rawOpen := &Mode{
		ClassName: "template-tag",
		Begin:     `\{\{\{\{`,
		End:       `\}\}\}\}`,
		Contains:  []*Mode{openingBlock},
		Starts: &Mode{
			End:         `\{\{\{\{\/`,
			ReturnEnd:   true,
			SubLanguage: []string{"xml"},
		},
	}
	rawOpen.beforeBegin = func(m *match, resp *Response) {
		if end := m.index + len(m.lexeme); end < len(m.input) && m.input[end] == '/' {
			resp.IgnoreMatch()
		}
	}

	elseMode := &Mode{ClassName: "template-tag", Begin: `\{\{`, End: `\}\}`, Keywords: Keywords{"keyword": "else"}}
	elseMode.beforeBegin = func(m *match, resp *Response) {
		if !startsWith(handlebarsElse, m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}
	elseIfMode := &Mode{ClassName: "template-tag", Begin: `\{\{`, End: `\}\}`, Keywords: Keywords{"keyword": "else if"}}
	elseIfMode.beforeBegin = func(m *match, resp *Response) {
		rest := m.input[m.index+len(m.lexeme):]
		if !strings.HasPrefix(rest, "else if") {
			resp.IgnoreMatch()
		}
	}

	return &Language{
		Name:              name,
		Aliases:           []string{"hbs", "html.hbs", "html.handlebars", "htmlbars"},
		CaseInsensitive:   true,
		DisableAutodetect: disableAutodetect,
		SubLanguage:       []string{"xml"},
		Contains: []*Mode{
			escape,
			preventEscape,
			CommentMode(`\{\{!--`, `--\}\}`),
			CommentMode(`\{\{!`, `\}\}`),
			rawOpen,
			{
				ClassName: "template-tag",
				Begin:     `\{\{\{\{\/`,
				End:       `\}\}\}\}`,
				Contains:  []*Mode{closingBlock},
			},
			{ClassName: "template-tag", Begin: `\{\{#`, End: `\}\}`, Contains: []*Mode{openingBlock}},
			elseMode,
			elseIfMode,
			{ClassName: "template-tag", Begin: `\{\{\/`, End: `\}\}`, Contains: []*Mode{closingBlock}},
			{ClassName: "template-variable", Begin: `\{\{\{`, End: `\}\}\}`, Contains: []*Mode{basicMustache}},
			{ClassName: "template-variable", Begin: `\{\{`, End: `\}\}`, Contains: []*Mode{basicMustache}},
		},
	}
}
