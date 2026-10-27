package highlight

import "strings"

func init() { RegisterLanguage("csharp", CSharp()) }

// CSharp returns the C# language definition (a port of languages/csharp.js).
func CSharp() *Language {
	builtIn := []string{"bool", "byte", "char", "decimal", "delegate", "double", "dynamic", "enum", "float", "int", "long", "nint", "nuint", "object", "sbyte", "short", "string", "ulong", "uint", "ushort"}
	functionModifiers := []string{"public", "private", "protected", "static", "internal", "protected", "abstract", "async", "extern", "override", "unsafe", "virtual", "new", "sealed", "partial"}
	literal := []string{"default", "false", "null", "true"}
	normal := []string{"abstract", "as", "base", "break", "case", "class", "const", "continue", "do", "else", "event", "explicit", "extern", "finally", "fixed", "for", "foreach", "goto", "if", "implicit", "in", "interface", "internal", "is", "lock", "namespace", "new", "operator", "out", "override", "params", "private", "protected", "public", "readonly", "record", "ref", "return", "sealed", "sizeof", "stackalloc", "static", "struct", "switch", "this", "throw", "try", "typeof", "unchecked", "unsafe", "using", "virtual", "void", "volatile", "while"}
	contextual := []string{"add", "alias", "and", "ascending", "async", "await", "by", "descending", "equals", "from", "get", "global", "group", "init", "into", "join", "let", "nameof", "not", "notnull", "on", "or", "orderby", "partial", "remove", "select", "set", "unmanaged", "value|0", "var", "when", "where", "with", "yield"}

	keywords := Keywords{
		"keyword":  strings.Join(append(append([]string{}, normal...), contextual...), " "),
		"built_in": strings.Join(builtIn, " "),
		"literal":  strings.Join(literal, " "),
	}

	titleMode := Variant(TitleMode, func(m *Mode) { m.Begin = `[a-zA-Z](\.?\w)*` })

	numbers := Zero(Mode{
		ClassName: "number",
		Variants: []*Mode{
			{ClassName: "number", Begin: `\b(0b[01']+)`},
			{ClassName: "number", Begin: `(-?)\b([\d']+(\.[\d']*)?|\.[\d']+)(u|U|l|L|ul|UL|f|F|b|B)`},
			{ClassName: "number", Begin: `(-?)(\b0[xX][a-fA-F0-9']+|(\b[\d']+(\.[\d']*)?|\.[\d']+)([eE][-+]?[\d']+)?)`},
		},
	})

	verbatim := &Mode{ClassName: "string", Begin: `@"`, End: `"`, Contains: []*Mode{{Begin: `""`}}}
	verbatimNoLF := Variant(verbatim, func(m *Mode) { m.Illegal = `\n` })
	subst := &Mode{ClassName: "subst", Begin: `\{`, End: `\}`, Keywords: keywords}
	substNoLF := Variant(subst, func(m *Mode) { m.Illegal = `\n` })
	interpolated := &Mode{
		ClassName: "string",
		Begin:     `\$"`,
		End:       `"`,
		Illegal:   `\n`,
		Contains:  []*Mode{{Begin: `\{\{`}, {Begin: `\}\}`}, BackslashEscape, substNoLF},
	}
	interpolatedVerbatim := &Mode{
		ClassName: "string",
		Begin:     `\$@"`,
		End:       `"`,
		Contains:  []*Mode{{Begin: `\{\{`}, {Begin: `\}\}`}, {Begin: `""`}, subst},
	}
	interpolatedVerbatimNoLF := Variant(interpolatedVerbatim, func(m *Mode) {
		m.Illegal = `\n`
		m.Contains = []*Mode{{Begin: `\{\{`}, {Begin: `\}\}`}, {Begin: `""`}, substNoLF}
	})

	subst.Contains = []*Mode{interpolatedVerbatim, interpolated, verbatim, AposStringMode, QuoteStringMode, numbers, CBlockCommentMode}
	substNoLF.Contains = []*Mode{interpolatedVerbatimNoLF, interpolated, verbatimNoLF, AposStringMode, QuoteStringMode, numbers, Variant(CBlockCommentMode, func(m *Mode) { m.Illegal = `\n` })}

	stringMode := &Mode{
		Variants: []*Mode{
			interpolatedVerbatim,
			interpolated,
			verbatim,
			Variant(AposStringMode, func(*Mode) {}),
			Variant(QuoteStringMode, func(*Mode) {}),
		},
	}

	genericModifier := &Mode{
		Begin: `<`,
		End:   `>`,
		Contains: []*Mode{
			{BeginKeywords: "in out"},
			titleMode,
		},
	}
	typeIdentRe := IdentRe + `(<` + IdentRe + `(\s*,\s*` + IdentRe + `)*>)?(\[\])?`
	atIdentifier := Zero(Mode{Begin: `@` + IdentRe})

	doctag := &Mode{
		ClassName: "doctag",
		Variants: []*Mode{
			{ClassName: "doctag", Begin: `///`},
			{ClassName: "doctag", Begin: `<!--|-->`},
			{ClassName: "doctag", Begin: `</?`, End: `>`},
		},
	}
	docComment := commentMode(`///`, `$`)
	docComment.ReturnBegin = true
	docComment.Contains = []*Mode{doctag, PhrasalWordsMode, Zero(Mode{ClassName: "doctag", Begin: `(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):`})}

	classMode := Zero(Mode{
		BeginKeywords: "class interface",
		End:           `[{;=]`,
		Illegal:       `[^\s:,]`,
		Contains: []*Mode{
			{BeginKeywords: "where class"},
			titleMode,
			genericModifier,
			CLineCommentMode,
			CBlockCommentMode,
		},
	})
	namespaceMode := Zero(Mode{
		BeginKeywords: "namespace",
		End:           `[{;=]`,
		Illegal:       `[^\s:]`,
		Contains:      []*Mode{titleMode, CLineCommentMode, CBlockCommentMode},
	})
	recordMode := Zero(Mode{
		BeginKeywords: "record",
		End:           `[{;=]`,
		Illegal:       `[^\s:]`,
		Contains:      []*Mode{titleMode, genericModifier, CLineCommentMode, CBlockCommentMode},
	})
	attribute := &Mode{
		ClassName:    "meta",
		Begin:        `^\s*\[`,
		ExcludeBegin: true,
		End:          `\]`,
		ExcludeEnd:   true,
		Contains:     []*Mode{{ClassName: "meta-string", Begin: `"`, End: `"`}},
	}
	functionMode := &Mode{
		ClassName:   "function",
		Begin:       `(` + typeIdentRe + `\s+)+` + IdentRe + `\s*(<.+>\s*)?\(`,
		ReturnBegin: true,
		End:         `\s*[{;=]`,
		ExcludeEnd:  true,
		Keywords:    keywords,
		Contains: []*Mode{
			{BeginKeywords: strings.Join(functionModifiers, " ")},
			{
				Begin:       IdentRe + `\s*(<.+>\s*)?\(`,
				ReturnBegin: true,
				Contains:    []*Mode{TitleMode, genericModifier},
			},
			{
				ClassName:    "params",
				Begin:        `\(`,
				End:          `\)`,
				ExcludeBegin: true,
				ExcludeEnd:   true,
				Keywords:     keywords,
				Contains:     []*Mode{stringMode, numbers, CBlockCommentMode},
			},
			CLineCommentMode,
			CBlockCommentMode,
		},
	}

	return &Language{
		Name:     "C#",
		Aliases:  []string{"cs", "c#"},
		Keywords: keywords,
		Illegal:  `::`,
		Contains: []*Mode{
			docComment,
			CLineCommentMode,
			CBlockCommentMode,
			{
				ClassName: "meta",
				Begin:     `#`,
				End:       `$`,
				Keywords:  Keywords{"meta-keyword": "if else elif endif define undef warning error line region endregion pragma checksum"},
			},
			stringMode,
			numbers,
			classMode,
			namespaceMode,
			recordMode,
			attribute,
			{BeginKeywords: "new return throw await else", HasRelevance: true, Relevance: 0},
			functionMode,
			atIdentifier,
		},
	}
}
