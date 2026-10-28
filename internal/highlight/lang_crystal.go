package highlight

func init() { RegisterLanguage("crystal", Crystal()) }

// Crystal returns the Crystal language definition (a port of languages/crystal.js).
func Crystal() *Language {
	const intSuffix = `(_?[ui](8|16|32|64|128))?`
	const floatSuffix = `(_?f(32|64))?`
	methodRe := "[a-zA-Z_]\\w*[!?=]?|[-+~]@|<<|>>|[=!]~|===?|<=>|[<>]=?|\\*\\*|[-/+%^&*~`|]|//|//=|&[-+*]=?|&\\*\\*|\\[\\][=?]?"
	const pathRe = `[A-Za-z_]\w*(::\w+)*(\?|!)?`

	keywords := Keywords{
		"$pattern": "[a-zA-Z_]\\w*[!?=]?",
		"keyword": "abstract alias annotation as as? asm begin break case class def do else elsif end ensure enum extend " +
			"for fun if include instance_sizeof is_a? lib macro module next nil? of out pointerof private " +
			"protected rescue responds_to? return require select self sizeof struct super then type typeof union " +
			"uninitialized unless until verbatim when while with yield __DIR__ __END_LINE__ __FILE__ __LINE__",
		"literal": "false nil true",
	}

	subst := &Mode{ClassName: "subst", Begin: `#\{`, End: `\}`, Keywords: keywords}

	recursiveParen := func(begin, end string) []*Mode {
		m := &Mode{Begin: begin, End: end}
		m.Contains = []*Mode{m}
		return []*Mode{m}
	}

	expansionBase := &Mode{ClassName: "template-variable", Keywords: keywords}
	expansionBase.Variants = []*Mode{
		Variant(expansionBase, func(m *Mode) { m.Begin = `\{\{`; m.End = `\}\}` }),
		Variant(expansionBase, func(m *Mode) { m.Begin = `\{%`; m.End = `%\}` }),
	}

	// STRING carries relevance 0; its variants inherit it (verified against the engine).
	stringBase := &Mode{ClassName: "string", HasRelevance: true, Relevance: 0, Contains: []*Mode{BackslashEscape, subst}}
	stringBase.Variants = []*Mode{
		Variant(stringBase, func(m *Mode) { m.Begin = `'`; m.End = `'` }),
		Variant(stringBase, func(m *Mode) { m.Begin = `"`; m.End = `"` }),
		Variant(stringBase, func(m *Mode) { m.Begin = "\x60"; m.End = "\x60" }),
		Variant(stringBase, func(m *Mode) { m.Begin = `%[Qwi]?\(`; m.End = `\)`; m.Contains = recursiveParen(`\(`, `\)`) }),
		Variant(stringBase, func(m *Mode) { m.Begin = `%[Qwi]?\[`; m.End = `\]`; m.Contains = recursiveParen(`\[`, `\]`) }),
		Variant(stringBase, func(m *Mode) { m.Begin = `%[Qwi]?\{`; m.End = `\}`; m.Contains = recursiveParen(`\{`, `\}`) }),
		Variant(stringBase, func(m *Mode) { m.Begin = `%[Qwi]?<`; m.End = `>`; m.Contains = recursiveParen(`<`, `>`) }),
		Variant(stringBase, func(m *Mode) { m.Begin = `%[Qwi]?\|`; m.End = `\|` }),
		Variant(stringBase, func(m *Mode) { m.Begin = `<<-\w+$`; m.End = `^\s*\w+$` }),
	}

	qBase := &Mode{ClassName: "string", HasRelevance: true, Relevance: 0}
	qBase.Variants = []*Mode{
		Variant(qBase, func(m *Mode) { m.Begin = `%q\(`; m.End = `\)`; m.Contains = recursiveParen(`\(`, `\)`) }),
		Variant(qBase, func(m *Mode) { m.Begin = `%q\[`; m.End = `\]`; m.Contains = recursiveParen(`\[`, `\]`) }),
		Variant(qBase, func(m *Mode) { m.Begin = `%q\{`; m.End = `\}`; m.Contains = recursiveParen(`\{`, `\}`) }),
		Variant(qBase, func(m *Mode) { m.Begin = `%q<`; m.End = `>`; m.Contains = recursiveParen(`<`, `>`) }),
		Variant(qBase, func(m *Mode) { m.Begin = `%q\|`; m.End = `\|` }),
		Variant(qBase, func(m *Mode) { m.Begin = `<<-'\w+'$`; m.End = `^\s*\w+$` }),
	}

	regexpInner := &Mode{ClassName: "regexp", Contains: []*Mode{BackslashEscape, subst}}
	slashVariant := Variant(regexpInner, func(m *Mode) { m.Begin = `/`; m.End = `/[a-z]*` })
	slashVariant.beforeBegin = func(m *match, resp *Response) {
		if end := m.index + len(m.lexeme); end < len(m.input) && m.input[end] == '/' {
			resp.IgnoreMatch()
		}
	}
	regexpInner.Variants = []*Mode{
		Variant(regexpInner, func(m *Mode) { m.Begin = `//[a-z]*`; m.HasRelevance = true; m.Relevance = 0 }),
		slashVariant,
	}
	regexpMode := &Mode{
		Begin:        `(\n|\b(case|if|select|unless|until|when|while)\b` + `|` + arcadeReStartersRe + `)\s*`,
		Keywords:     Keywords{"keyword": "case if select unless until when while"},
		Contains:     []*Mode{regexpInner},
		HasRelevance: true,
		Relevance:    0,
	}
	regexpMode.beforeBegin = func(m *match, resp *Response) {
		if len(m.input)-m.index >= 2 && m.input[m.index] == '%' && m.input[m.index+1] == '}' {
			resp.IgnoreMatch()
		}
	}

	// REGEXP2 carries relevance 0; its variants inherit it.
	regexp2Base := &Mode{ClassName: "regexp", HasRelevance: true, Relevance: 0, Contains: []*Mode{BackslashEscape, subst}}
	regexp2Base.Variants = []*Mode{
		Variant(regexp2Base, func(m *Mode) { m.Begin = `%r\(`; m.End = `\)`; m.Contains = recursiveParen(`\(`, `\)`) }),
		Variant(regexp2Base, func(m *Mode) { m.Begin = `%r\[`; m.End = `\]`; m.Contains = recursiveParen(`\[`, `\]`) }),
		Variant(regexp2Base, func(m *Mode) { m.Begin = `%r\{`; m.End = `\}`; m.Contains = recursiveParen(`\{`, `\}`) }),
		Variant(regexp2Base, func(m *Mode) { m.Begin = `%r<`; m.End = `>`; m.Contains = recursiveParen(`<`, `>`) }),
		Variant(regexp2Base, func(m *Mode) { m.Begin = `%r\|`; m.End = `\|` }),
	}

	attribute := &Mode{
		ClassName: "meta",
		Begin:     `@\[`,
		End:       `\]`,
		Contains:  []*Mode{Variant(QuoteStringMode, func(m *Mode) { m.ClassName = "meta-string" })},
	}

	titlePath := func() *Mode { return Variant(TitleMode, func(m *Mode) { m.Begin = pathRe }) }

	classOne := &Mode{
		ClassName:     "class",
		BeginKeywords: "class module struct",
		End:           `$|;`,
		Illegal:       `=`,
		Contains:      []*Mode{HashCommentMode, titlePath(), {Begin: `<`}},
	}
	classTwo := &Mode{
		ClassName:     "class",
		BeginKeywords: "lib enum union",
		End:           `$|;`,
		Illegal:       `=`,
		Contains:      []*Mode{HashCommentMode, titlePath()},
	}
	annotation := &Mode{
		BeginKeywords: "annotation",
		End:           `$|;`,
		Illegal:       `=`,
		Contains:      []*Mode{HashCommentMode, titlePath()},
		HasRelevance:  true,
		Relevance:     2,
	}
	defMode := &Mode{
		ClassName:     "function",
		BeginKeywords: "def",
		End:           `\B\b`,
		Contains: []*Mode{
			Variant(TitleMode, func(m *Mode) { m.Begin = methodRe; m.EndsParent = true }),
		},
	}
	funMacro := &Mode{
		ClassName:     "function",
		BeginKeywords: "fun macro",
		End:           `\B\b`,
		Contains: []*Mode{
			Variant(TitleMode, func(m *Mode) { m.Begin = methodRe; m.EndsParent = true }),
		},
		HasRelevance: true,
		Relevance:    2,
	}

	symbolOne := &Mode{ClassName: "symbol", Begin: UnderscoreIdentRe + `(!|\?)?:`, HasRelevance: true, Relevance: 0}
	symbolTwo := &Mode{
		ClassName:    "symbol",
		Begin:        `:`,
		Contains:     []*Mode{stringBase, {Begin: methodRe}},
		HasRelevance: true,
		Relevance:    0,
	}

	floatVariant := Variant(&Mode{ClassName: "number", HasRelevance: true, Relevance: 0}, func(m *Mode) {
		m.Begin = `\b([1-9][0-9_]*[0-9]|[0-9])(\.[0-9][0-9_]*)?([eE]_?[-+]?[0-9_]*)?` + floatSuffix
	})
	floatVariant.beforeBegin = func(m *match, resp *Response) {
		if end := m.index + len(m.lexeme); end < len(m.input) && m.input[end] == '_' {
			resp.IgnoreMatch()
		}
	}
	numberBase := &Mode{ClassName: "number", HasRelevance: true, Relevance: 0}
	numberBase.Variants = []*Mode{
		Variant(numberBase, func(m *Mode) { m.Begin = `\b0b([01_]+)` + intSuffix }),
		Variant(numberBase, func(m *Mode) { m.Begin = `\b0o([0-7_]+)` + intSuffix }),
		Variant(numberBase, func(m *Mode) { m.Begin = `\b0x([A-Fa-f0-9_]+)` + intSuffix }),
		floatVariant,
		Variant(numberBase, func(m *Mode) { m.Begin = `\b([1-9][0-9_]*|0)` + intSuffix }),
	}

	defaults := []*Mode{
		expansionBase,
		stringBase,
		qBase,
		regexp2Base,
		regexpMode,
		attribute,
		HashCommentMode,
		classOne,
		classTwo,
		annotation,
		defMode,
		funMacro,
		symbolOne,
		symbolTwo,
		numberBase,
	}
	subst.Contains = defaults
	for _, v := range expansionBase.Variants {
		v.Contains = defaults[1:]
	}

	return &Language{
		Name:     "Crystal",
		Aliases:  []string{"cr"},
		Keywords: keywords,
		Contains: defaults,
	}
}
