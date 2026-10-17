package highlight

import "strings"

func init() { RegisterLanguage("python", Python()) }

// Python returns the Python language definition (a port of languages/python.js).
func Python() *Language {
	reserved := []string{
		"and", "as", "assert", "async", "await", "break", "class", "continue",
		"def", "del", "elif", "else", "except", "finally", "for", "from",
		"global", "if", "import", "in", "is", "lambda", "nonlocal|10", "not",
		"or", "pass", "raise", "return", "try", "while", "with", "yield",
	}
	builtIns := []string{
		"__import__", "abs", "all", "any", "ascii", "bin", "bool", "breakpoint",
		"bytearray", "bytes", "callable", "chr", "classmethod", "compile",
		"complex", "delattr", "dict", "dir", "divmod", "enumerate", "eval",
		"exec", "filter", "float", "format", "frozenset", "getattr", "globals",
		"hasattr", "hash", "help", "hex", "id", "input", "int", "isinstance",
		"issubclass", "iter", "len", "list", "locals", "map", "max", "memoryview",
		"min", "next", "object", "oct", "open", "ord", "pow", "print", "property",
		"range", "repr", "reversed", "round", "set", "setattr", "slice", "sorted",
		"staticmethod", "str", "sum", "super", "tuple", "type", "vars", "zip",
	}
	literals := []string{"__debug__", "Ellipsis", "False", "None", "NotImplemented", "True"}
	types := []string{"Any", "Callable", "Coroutine", "Dict", "List", "Literal", "Generic", "Optional", "Sequence", "Set", "Tuple", "Type", "Union"}

	keywords := Keywords{
		"$pattern": `[A-Za-z]\w+|__\w+__`,
		"keyword":  strings.Join(reserved, " "),
		"built_in": strings.Join(builtIns, " "),
		"literal":  strings.Join(literals, " "),
		"type":     strings.Join(types, " "),
	}

	prompt := &Mode{ClassName: "meta", Begin: `^(>>>|\.\.\.) `}
	subst := &Mode{
		ClassName: "subst",
		Begin:     `\{`,
		End:       `\}`,
		Keywords:  keywords,
		Illegal:   `#`,
	}
	literalBracket := Zero(Mode{Begin: `\{\{`})

	stringMode := &Mode{
		ClassName: "string",
		Contains:  []*Mode{BackslashEscape},
		Variants: []*Mode{
			{ClassName: "string", Begin: `([uU]|[bB]|[rR]|[bB][rR]|[rR][bB])?'''`, End: `'''`, Contains: []*Mode{BackslashEscape, prompt}, HasRelevance: true, Relevance: 10},
			{ClassName: "string", Begin: `([uU]|[bB]|[rR]|[bB][rR]|[rR][bB])?"""`, End: `"""`, Contains: []*Mode{BackslashEscape, prompt}, HasRelevance: true, Relevance: 10},
			{ClassName: "string", Begin: `([fF][rR]|[rR][fF]|[fF])'''`, End: `'''`, Contains: []*Mode{BackslashEscape, prompt, literalBracket, subst}},
			{ClassName: "string", Begin: `([fF][rR]|[rR][fF]|[fF])"""`, End: `"""`, Contains: []*Mode{BackslashEscape, prompt, literalBracket, subst}},
			{ClassName: "string", Begin: `([uU]|[rR])'`, End: `'`, Contains: []*Mode{BackslashEscape}, HasRelevance: true, Relevance: 10},
			{ClassName: "string", Begin: `([uU]|[rR])"`, End: `"`, Contains: []*Mode{BackslashEscape}, HasRelevance: true, Relevance: 10},
			{ClassName: "string", Begin: `([bB]|[bB][rR]|[rR][bB])'`, End: `'`, Contains: []*Mode{BackslashEscape}},
			{ClassName: "string", Begin: `([bB]|[bB][rR]|[rR][bB])"`, End: `"`, Contains: []*Mode{BackslashEscape}},
			{ClassName: "string", Begin: `([fF][rR]|[rR][fF]|[fF])'`, End: `'`, Contains: []*Mode{BackslashEscape, literalBracket, subst}},
			{ClassName: "string", Begin: `([fF][rR]|[rR][fF]|[fF])"`, End: `"`, Contains: []*Mode{BackslashEscape, literalBracket, subst}},
			Variant(AposStringMode, func(*Mode) {}),
			Variant(QuoteStringMode, func(*Mode) {}),
		},
	}

	const digitPart = `[0-9](_?[0-9])*`
	pointFloat := `(\b(` + digitPart + `))?\.(` + digitPart + `)|\b(` + digitPart + `)\.`
	number := Zero(Mode{
		ClassName: "number",
		Variants: []*Mode{
			{ClassName: "number", Begin: `(\b(` + digitPart + `)|(` + pointFloat + `))[eE][+-]?(` + digitPart + `)[jJ]?\b`},
			{ClassName: "number", Begin: `(` + pointFloat + `)[jJ]?`},
			{ClassName: "number", Begin: `\b([1-9](_?[0-9])*|0+(_?0)*)[lLjJ]?\b`},
			{ClassName: "number", Begin: `\b0[bB](_?[01])+[lL]?\b`},
			{ClassName: "number", Begin: `\b0[oO](_?[0-7])+[lL]?\b`},
			{ClassName: "number", Begin: `\b0[xX](_?[0-9a-fA-F])+[lL]?\b`},
			{ClassName: "number", Begin: `\b(` + digitPart + `)[jJ]\b`},
		},
	})

	// The reference begins this mode with a `(?=# type:)` lookahead. Consuming
	// `# type:` and returning before it keeps the same span.
	commentType := &Mode{
		ClassName:   "comment",
		Begin:       `# type:`,
		ReturnBegin: true,
		End:         `$`,
		Keywords:    keywords,
		Contains: []*Mode{
			{Begin: `# type:`},
			{Begin: `#`, End: `\b\B`, EndsWithParent: true},
		},
	}

	params := &Mode{
		ClassName: "params",
		Variants: []*Mode{
			{ClassName: "", Begin: `\(\s*\)`, Skip: true},
			{
				ClassName:    "params",
				Begin:        `\(`,
				End:          `\)`,
				ExcludeBegin: true,
				ExcludeEnd:   true,
				Keywords:     keywords,
				Contains:     []*Mode{Self, prompt, number, stringMode, HashCommentMode},
			},
		},
	}
	subst.Contains = []*Mode{stringMode, number, prompt}

	funcDef := &Mode{
		End:      `:`,
		Illegal:  `[${=;\n,]`,
		Contains: []*Mode{UnderscoreTitleMode, params, {Begin: `->`, EndsWithParent: true, Keywords: keywords}},
	}
	defOrClass := &Mode{
		Variants: []*Mode{
			Variant(funcDef, func(m *Mode) { m.ClassName = "function"; m.BeginKeywords = "def" }),
			Variant(funcDef, func(m *Mode) { m.ClassName = "class"; m.BeginKeywords = "class" }),
		},
	}

	// The reference's end is `(?=#)|$`; the zero-width-before-`#` alternative is
	// not expressible in RE2, so an end-of-line terminator is used.
	decorator := &Mode{
		ClassName: "meta",
		Begin:     `^[\t ]*@`,
		End:       `$`,
		Contains:  []*Mode{number, params, stringMode},
	}

	return &Language{
		Name:    "Python",
		Aliases: []string{"py", "gyp", "ipython"},
		Mode: Mode{
			Keywords: keywords,
			Illegal:  `(<\/|->|\?)|=>`,
			Contains: []*Mode{
				prompt,
				number,
				{Begin: `\bself\b`},
				{BeginKeywords: "if", HasRelevance: true, Relevance: 0},
				stringMode,
				commentType,
				HashCommentMode,
				defOrClass,
				decorator,
			},
		},
	}
}
