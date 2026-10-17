package highlight

import "regexp"

func init() { RegisterLanguage("markdown", Markdown()) }

// Markdown returns the Markdown language definition (a port of
// languages/markdown.js).
//
// The fenced-code variants use `\1` backreferences (the closing fence run must
// repeat the opening run); RE2 has no backreferences, so the closing fence is
// matched as `{3,}` of the same character. That accepts a closing run of a
// different length than the opening, which the reference rejects; the common
// equal-length case is unaffected.
func Markdown() *Language {
	const bt = "`"

	inlineHTML := Zero(Mode{Begin: `</?[A-Za-z_]`, End: `>`, SubLanguage: []string{"xml"}})
	horizontalRule := &Mode{Begin: `^[-\*]{3,}`, End: `$`}
	code := &Mode{
		ClassName: "code",
		Variants: []*Mode{
			{ClassName: "code", Begin: "(" + bt + "{3,})[^" + bt + "](.|\\n)*?" + bt + "{3,}" + bt + "*[ ]*"},
			{ClassName: "code", Begin: `(~{3,})[^~](.|\n)*?~{3,}~*[ ]*`},
			{ClassName: "code", Begin: bt + bt + bt, End: bt + bt + bt + `+[ ]*$`},
			{ClassName: "code", Begin: `~~~`, End: `~~~+[ ]*$`},
			{ClassName: "code", Begin: bt + `.+?` + bt},
			{
				ClassName:    "code",
				Begin:        `^( {4}|\t)`,
				ReturnBegin:  true,
				HasRelevance: true,
				Relevance:    0,
				Contains:     []*Mode{{Begin: `^( {4}|\t)`, End: `(\n)$`}},
			},
		},
	}
	list := &Mode{
		ClassName:  "bullet",
		Begin:      `^[ \t]*([*+-]|(\d+\.))`,
		End:        `\s+`,
		ExcludeEnd: true,
	}
	list.beforeBegin = func(m *match, resp *Response) {
		next := m.index + len(m.lexeme)
		if next >= len(m.input) || !isWhitespaceByte(m.input[next]) {
			resp.IgnoreMatch()
		}
	}
	linkReference := &Mode{
		Begin:       `^\[[^\n]+\]:`,
		ReturnBegin: true,
		Contains: []*Mode{
			{ClassName: "symbol", Begin: `\[`, End: `\]`, ExcludeBegin: true, ExcludeEnd: true},
			{ClassName: "link", Begin: `:\s*`, End: `$`, ExcludeBegin: true},
		},
	}
	const urlScheme = `[A-Za-z][A-Za-z0-9+.-]*`
	// The reference puts returnBegin/contains on the outer mode and only begin
	// on each variant; Variant clones the base so the variants inherit them.
	linkBase := &Mode{
		ReturnBegin: true,
		Contains: []*Mode{
			{ClassName: "string", Begin: `\[`, End: `\]`, ExcludeBegin: true, ReturnEnd: true},
			{ClassName: "link", Begin: `\]\(`, End: `\)`, ExcludeBegin: true, ExcludeEnd: true},
			{ClassName: "symbol", Begin: `\]\[`, End: `\]`, ExcludeBegin: true, ExcludeEnd: true},
		},
	}
	linkBase.Variants = []*Mode{
		Variant(linkBase, func(m *Mode) { m.Begin = `\[.+?\]\[.*?\]`; m.HasRelevance = true; m.Relevance = 0 }),
		Variant(linkBase, func(m *Mode) {
			m.Begin = `\[.+?\]\(((data|javascript|mailto):|(?:http|ftp)s?:\/\/).*?\)`
			m.HasRelevance = true
			m.Relevance = 2
		}),
		Variant(linkBase, func(m *Mode) {
			m.Begin = `\[.+?\]\(` + urlScheme + `:\/\/.*?\)`
			m.HasRelevance = true
			m.Relevance = 2
		}),
		Variant(linkBase, func(m *Mode) {
			m.Begin = `\[.+?\]\([./?&#].*?\)`
			m.HasRelevance = true
			m.Relevance = 1
		}),
		Variant(linkBase, func(m *Mode) { m.Begin = `\[.+?\]\(.*?\)`; m.HasRelevance = true; m.Relevance = 0 }),
	}
	link := linkBase
	bold := &Mode{
		ClassName: "strong",
		Variants: []*Mode{
			{ClassName: "strong", Begin: `_{2}`, End: `_{2}`},
			{ClassName: "strong", Begin: `\*{2}`, End: `\*{2}`},
		},
	}
	italic := &Mode{
		ClassName: "emphasis",
		Variants: []*Mode{
			{ClassName: "emphasis", Begin: `\*`, End: `\*`},
			{ClassName: "emphasis", Begin: `_`, End: `_`, HasRelevance: true, Relevance: 0},
		},
	}
	italicVariantGuard := func(m *match, resp *Response) {
		next := m.index + len(m.lexeme)
		if next < len(m.input) && m.input[next] == m.lexeme[0] {
			resp.IgnoreMatch()
		}
	}
	for _, v := range italic.Variants {
		v.beforeBegin = italicVariantGuard
	}
	containable := []*Mode{inlineHTML, link}
	bold.Contains = append([]*Mode{italic}, containable...)
	italic.Contains = append([]*Mode{bold}, containable...)
	containable = append(containable, bold, italic)

	// The reference's setext variant begins with the zero-width lookahead
	// `(?=^.+?\n[=-]{2,}$)`; RE2 has none, so match `^` and guard on the two
	// lines. A consuming begin with returnBegin instead would close the section
	// before the underline.
	setextGuard := regexp.MustCompile(`(?m)\A.+?\n[=-]{2,}$`)
	setext := &Mode{
		ClassName: "section",
		Begin:     `^`,
		Contains: []*Mode{
			{Begin: `^[=-]+$`},
			// `[^\n]+` rather than the reference's zero-width `^`: at end of input
			// a sliced search makes a bare `^` match at the slice boundary, and that
			// zero-width rule then beats the section's own end and the section never
			// closes.
			{Begin: `[^\n]+`, End: `\n`, Contains: containable},
		},
	}
	setext.beforeBegin = func(m *match, resp *Response) {
		if !setextGuard.MatchString(m.input[m.index:]) {
			resp.IgnoreMatch()
		}
	}
	header := &Mode{
		ClassName: "section",
		Variants: []*Mode{
			{ClassName: "section", Begin: `^#{1,6}`, End: `$`, Contains: containable},
			setext,
		},
	}
	blockquote := &Mode{ClassName: "quote", Begin: `^>\s+`, Contains: containable, End: `$`}

	return &Language{
		Name:    "Markdown",
		Aliases: []string{"md", "mkdown", "mkd"},
		Mode: Mode{
			Contains: []*Mode{
				header,
				inlineHTML,
				list,
				bold,
				italic,
				blockquote,
				code,
				horizontalRule,
				link,
				linkReference,
			},
		},
	}
}
