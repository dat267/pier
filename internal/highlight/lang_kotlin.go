package highlight

func init() { RegisterLanguage("kotlin", Kotlin()) }

// Kotlin returns the Kotlin language definition (a port of languages/kotlin.js).
func Kotlin() *Language {
	keywords := Keywords{
		"keyword": "abstract as val var vararg get set class object open private protected public noinline " +
			"crossinline dynamic final enum if else do while for when throw try catch finally " +
			"import package is in fun override companion reified inline lateinit init " +
			"interface annotation data sealed internal infix operator out by constructor super " +
			"tailrec where const inner suspend typealias external expect actual",
		"built_in": "Byte Short Char Int Long Boolean Float Double Void Unit Nothing",
		"literal":  "true false null",
	}

	keywordsWithLabel := &Mode{
		ClassName: "keyword",
		Begin:     `\b(break|continue|return|this)\b`,
		Starts: &Mode{
			Contains: []*Mode{{ClassName: "symbol", Begin: `@\w+`}},
		},
	}
	label := &Mode{ClassName: "symbol", Begin: UnderscoreIdentRe + `@`}

	subst := &Mode{ClassName: "subst", Begin: `\$\{`, End: `\}`, Contains: []*Mode{CNumberMode}}
	variable := &Mode{ClassName: "variable", Begin: `\$` + UnderscoreIdentRe}
	stringMode := &Mode{
		ClassName: "string",
		Variants: []*Mode{
			{ClassName: "string", Begin: `"""`, End: `"""`, Contains: []*Mode{variable, subst}},
			{ClassName: "string", Begin: `'`, End: `'`, Illegal: `\n`, Contains: []*Mode{BackslashEscape}},
			{ClassName: "string", Begin: `"`, End: `"`, Illegal: `\n`, Contains: []*Mode{BackslashEscape, variable, subst}},
		},
	}
	subst.Contains = append(subst.Contains, stringMode)

	annotationUseSite := &Mode{
		ClassName: "meta",
		Begin:     `@(?:file|property|field|get|set|receiver|param|setparam|delegate)\s*:(?:\s*` + UnderscoreIdentRe + `)?`,
	}
	annotation := &Mode{
		ClassName: "meta",
		Begin:     `@` + UnderscoreIdentRe,
		Contains: []*Mode{
			{Begin: `\(`, End: `\)`, Contains: []*Mode{Variant(stringMode, func(m *Mode) { m.ClassName = "meta-string" })}},
		},
	}

	nestedComment := commentMode(`/\*`, `\*/`)
	nestedComment.Contains = []*Mode{CBlockCommentMode, PhrasalWordsMode, Zero(Mode{ClassName: "doctag", Begin: `(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):`})}

	// The reference's KOTLIN_PAREN_TYPE has a variant that contains itself.
	parenType := &Mode{}
	typeLeaf := &Mode{ClassName: "type", Begin: UnderscoreIdentRe}
	parenLeaf := &Mode{Begin: `\(`, End: `\)`}
	parenType.Variants = []*Mode{typeLeaf, parenLeaf}
	parenLeaf.Contains = []*Mode{parenType}

	function := &Mode{
		ClassName:     "function",
		BeginKeywords: "fun",
		End:           `[(]|$`,
		ReturnBegin:   true,
		ExcludeEnd:    true,
		Keywords:      keywords,
		HasRelevance:  true,
		Relevance:     5,
		Contains: []*Mode{
			{Begin: UnderscoreIdentRe + `\s*\(`, ReturnBegin: true, HasRelevance: true, Relevance: 0, Contains: []*Mode{UnderscoreTitleMode}},
			Zero(Mode{ClassName: "type", Begin: `<`, End: `>`, Keywords: Keywords{"keyword": "reified"}}),
			{
				ClassName:    "params",
				Begin:        `\(`,
				End:          `\)`,
				EndsParent:   true,
				Keywords:     keywords,
				HasRelevance: true,
				Relevance:    0,
				Contains: []*Mode{
					{
						Begin:          `:`,
						End:            `[=,\/]`,
						EndsWithParent: true,
						HasRelevance:   true,
						Relevance:      0,
						Contains:       []*Mode{parenType, CLineCommentMode, nestedComment},
					},
					CLineCommentMode,
					nestedComment,
					annotationUseSite,
					annotation,
					stringMode,
					CNumberMode,
				},
			},
			nestedComment,
		},
	}

	classMode := &Mode{
		ClassName:     "class",
		BeginKeywords: "class interface trait",
		End:           `[:\{(]|$`,
		ExcludeEnd:    true,
		Illegal:       "extends implements",
		Contains: []*Mode{
			{BeginKeywords: "public protected internal private constructor"},
			UnderscoreTitleMode,
			Zero(Mode{ClassName: "type", Begin: `<`, End: `>`, ExcludeBegin: true, ExcludeEnd: true}),
			{ClassName: "type", Begin: `[,:]\s*`, End: `[<\(,]|$`, ExcludeBegin: true, ReturnEnd: true},
			annotationUseSite,
			annotation,
		},
	}

	javadoc := Zero(Mode{
		ClassName: "comment",
		Begin:     `/\*\*`,
		End:       `\*/`,
		Contains:  []*Mode{{ClassName: "doctag", Begin: `@[A-Za-z]+`}},
	})

	return &Language{
		Name:     "Kotlin",
		Aliases:  []string{"kt", "kts"},
		Keywords: keywords,
		Contains: []*Mode{
			javadoc,
			CLineCommentMode,
			nestedComment,
			keywordsWithLabel,
			label,
			annotationUseSite,
			annotation,
			function,
			classMode,
			stringMode,
			{ClassName: "meta", Begin: `^#!/usr/bin/env`, End: `$`, Illegal: `\n`},
			javaNumeric(),
		},
	}
}
