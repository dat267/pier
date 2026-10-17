package highlight

func init() { RegisterLanguage("reasonml", ReasonML()) }

// ReasonML returns the ReasonML language definition (a port of
// languages/reasonml.js).
func ReasonML() *Language {
	const reIdent = `~?[a-z$_][0-9a-zA-Z$_]*`
	const reModuleIdent = "\x60?[A-Z$_][0-9a-zA-Z$_]*"
	const reParamTypeParam = `\'?[a-z$_][0-9a-z$_]*`
	reParamType := `\s*:\s*[a-z$_][0-9a-z$_]*(\(\s*(` + reParamTypeParam + `\s*(,` + reParamTypeParam + `\s*)*)?\))?`
	reParam := reIdent + `(` + reParamType + `){0,2}`
	const reOperator = `(\|\||\+\+|\*\*|\+\.|\*|\/|\*\.|\/\.|\.\.\.|\|>|&&|==|===)`
	const reOperatorSpaced = `\s+` + reOperator + `\s+`
	const reNumber = `\b(0[xX][a-fA-F0-9_]+[Lln]?|0[oO][0-7_]+[Lln]?|0[bB][01_]+[Lln]?|[0-9][0-9_]*([Lln]|(\.[0-9_]*)?([eE][-+]?[0-9_]+)?)?)`

	keywords := Keywords{
		"keyword": "and as asr assert begin class constraint do done downto else end exception external for fun function " +
			"functor if in include inherit initializer land lazy let lor lsl lsr lxor match method mod module " +
			"mutable new nonrec object of open or private rec sig struct then to try type val virtual when while " +
			"with",
		"built_in": "array bool bytes char exn|5 float int int32 int64 list lazy_t|5 nativeint|5 ref string unit ",
		"literal":  "true false",
	}

	numberMode := &Mode{ClassName: "number", HasRelevance: true, Relevance: 0}
	numberMode.Variants = []*Mode{
		Variant(numberMode, func(m *Mode) { m.Begin = reNumber }),
		Variant(numberMode, func(m *Mode) { m.Begin = `\(-` + reNumber + `\)` }),
	}

	operatorMode := &Mode{ClassName: "operator", Begin: reOperator, HasRelevance: true, Relevance: 0}

	listContents := []*Mode{
		{ClassName: "identifier", HasRelevance: true, Relevance: 0, Begin: reIdent},
		operatorMode,
		numberMode,
	}

	moduleAccessContents := []*Mode{
		QuoteStringMode,
		operatorMode,
		{
			ClassName:   "module",
			Begin:       `\b` + reModuleIdent,
			ReturnBegin: true,
			End:         `\.`,
			Contains:    []*Mode{{ClassName: "identifier", Begin: reModuleIdent, HasRelevance: true, Relevance: 0}},
		},
	}

	paramsContents := []*Mode{
		{
			ClassName:    "module",
			Begin:        `\b` + reModuleIdent,
			ReturnBegin:  true,
			End:          `\.`,
			HasRelevance: true,
			Relevance:    0,
			Contains:     []*Mode{{ClassName: "identifier", Begin: reModuleIdent, HasRelevance: true, Relevance: 0}},
		},
	}

	paramsMode := &Mode{
		ClassName:    "params",
		Begin:        reIdent,
		End:          `(,|\n|\))`,
		HasRelevance: true,
		Relevance:    0,
		Contains: []*Mode{
			operatorMode,
			{
				ClassName:    "typing",
				Begin:        `:`,
				End:          `(,|\n)`,
				ReturnBegin:  true,
				HasRelevance: true,
				Relevance:    0,
				Contains:     paramsContents,
			},
		},
	}

	functionBlock := &Mode{ClassName: "function", Keywords: keywords, HasRelevance: true, Relevance: 0}
	functionBlock.Variants = []*Mode{
		Variant(functionBlock, func(m *Mode) {
			m.Begin = `\s(\(\.?.*?\)|` + reIdent + `)\s*=>`
			m.End = `\s*=>`
			m.ReturnBegin = true
			paramsBase := &Mode{ClassName: "params"}
			paramsBase.Variants = []*Mode{
				Variant(paramsBase, func(p *Mode) { p.Begin = reIdent }),
				Variant(paramsBase, func(p *Mode) { p.Begin = reParam }),
				Variant(paramsBase, func(p *Mode) { p.Begin = `\(\s*\)` }),
			}
			m.Contains = []*Mode{paramsBase}
		}),
		Variant(functionBlock, func(m *Mode) {
			m.Begin = `\s\(\.?[^;\|]*\)\s*=>`
			m.End = `\s=>`
			m.ReturnBegin = true
			m.Contains = []*Mode{
				{ClassName: "params", HasRelevance: true, Relevance: 0, Variants: []*Mode{paramsMode}},
			}
		}),
		Variant(functionBlock, func(m *Mode) { m.Begin = `\(\.\s` + reIdent + `\)\s*=>` }),
	}

	moduleAccessContents = append(moduleAccessContents, functionBlock)

	constructorMode := &Mode{
		ClassName: "constructor",
		Begin:     reModuleIdent + `\(`,
		End:       `\)`,
		Illegal:   `\n`,
		Keywords:  keywords,
		Contains: []*Mode{
			QuoteStringMode,
			operatorMode,
			{ClassName: "params", Begin: `\b` + reIdent},
		},
	}

	patternMatchBlock := &Mode{
		ClassName:    "pattern-match",
		Begin:        `\|`,
		ReturnBegin:  true,
		Keywords:     keywords,
		End:          `=>`,
		HasRelevance: true,
		Relevance:    0,
		Contains: []*Mode{
			constructorMode,
			operatorMode,
			{HasRelevance: true, Relevance: 0, ClassName: "constructor", Begin: reModuleIdent},
		},
	}

	moduleAccess := &Mode{
		ClassName:   "module-access",
		Keywords:    keywords,
		ReturnBegin: true,
		Contains:    moduleAccessContents,
	}
	moduleAccess.Variants = []*Mode{
		Variant(moduleAccess, func(m *Mode) {
			m.Begin = `\b(` + reModuleIdent + `\.)+` + reIdent
		}),
		Variant(moduleAccess, func(m *Mode) {
			m.Begin = `\b(` + reModuleIdent + `\.)+\(`
			m.End = `\)`
			m.ReturnBegin = true
			m.Contains = append([]*Mode{
				functionBlock,
				{Begin: `\(`, End: `\)`, Skip: true},
			}, moduleAccessContents...)
		}),
		Variant(moduleAccess, func(m *Mode) {
			m.Begin = `\b(` + reModuleIdent + `\.)+\{`
			m.End = `\}`
		}),
	}

	paramsContents = append(paramsContents, moduleAccess)

	comment := CommentMode(`/\*`, `\*/`)
	comment.Illegal = `^(#,\/\/)`

	return &Language{
		Name:    "ReasonML",
		Aliases: []string{"re"},
		Mode: Mode{
			Keywords: keywords,
			Illegal:  `(:-|:=|\$\{|\+=)`,
			Contains: []*Mode{
				comment,
				{ClassName: "character", Begin: `\'(\\[^\']+|[^\'])\'`, Illegal: `\n`, HasRelevance: true, Relevance: 0},
				QuoteStringMode,
				{ClassName: "literal", Begin: `\(\)`, HasRelevance: true, Relevance: 0},
				{ClassName: "literal", Begin: `\[\|`, End: `\|\]`, HasRelevance: true, Relevance: 0, Contains: listContents},
				{ClassName: "literal", Begin: `\[`, End: `\]`, HasRelevance: true, Relevance: 0, Contains: listContents},
				constructorMode,
				{ClassName: "operator", Begin: reOperatorSpaced, Illegal: `-->`, HasRelevance: true, Relevance: 0},
				numberMode,
				CLineCommentMode,
				patternMatchBlock,
				functionBlock,
				{
					ClassName:    "module-def",
					Begin:        `\bmodule\s+` + reIdent + `\s+` + reModuleIdent + `\s+=\s+\{`,
					End:          `\}`,
					ReturnBegin:  true,
					Keywords:     keywords,
					HasRelevance: true,
					Relevance:    0,
					Contains: append([]*Mode{
						{ClassName: "module", HasRelevance: true, Relevance: 0, Begin: reModuleIdent},
						{Begin: `\{`, End: `\}`, Skip: true},
					}, moduleAccessContents...),
				},
				moduleAccess,
			},
		},
	}
}
