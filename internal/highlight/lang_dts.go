package highlight

func init() { RegisterLanguage("dts", DTS()) }

// DTS returns the Device Tree language definition (a port of languages/dts.js).
func DTS() *Language {
	stringsMode := &Mode{ClassName: "string"}
	stringsMode.Variants = []*Mode{
		Variant(stringsMode, func(m *Mode) {
			m.Begin = `((u8?|U)|L)?"`
			m.End = `"`
			m.Illegal = `\n`
			m.Contains = []*Mode{BackslashEscape}
		}),
		Variant(stringsMode, func(m *Mode) {
			m.Begin = `(u8?|U)?R"`
			m.End = `"`
			m.Contains = []*Mode{BackslashEscape}
		}),
		Variant(stringsMode, func(m *Mode) { m.Begin = `'\\?.`; m.End = `'`; m.Illegal = `.` }),
	}

	numbers := &Mode{ClassName: "number", HasRelevance: true, Relevance: 0}
	numbers.Variants = []*Mode{
		Variant(numbers, func(m *Mode) { m.Begin = `\b(\d+(\.\d*)?|\.\d+)(u|U|l|L|ul|UL|f|F)` }),
		Variant(numbers, func(m *Mode) { m.Begin = CNumberRe }),
	}

	preprocessor := &Mode{
		ClassName: "meta",
		Begin:     `#`,
		End:       `$`,
		Keywords:  Keywords{"meta-keyword": "if else elif endif define undef ifdef ifndef"},
		Contains: []*Mode{
			{Begin: `\\\n`, HasRelevance: true, Relevance: 0},
			{
				BeginKeywords: "include",
				End:           `$`,
				Keywords:      Keywords{"meta-keyword": "include"},
				Contains: []*Mode{
					Variant(stringsMode, func(m *Mode) { m.ClassName = "meta-string" }),
					{ClassName: "meta-string", Begin: `<`, End: `>`, Illegal: `\n`},
				},
			},
			stringsMode,
			CLineCommentMode,
			CBlockCommentMode,
		},
	}

	reference := &Mode{ClassName: "variable", Begin: `&[a-z\d_]*\b`}
	keyword := &Mode{ClassName: "meta-keyword", Begin: `/[a-z][a-z\d-]*/`}
	label := &Mode{ClassName: "symbol", Begin: `^\s*[a-zA-Z_][a-zA-Z\d_]*:`}
	cellProperty := &Mode{
		ClassName: "params",
		Begin:     `<`,
		End:       `>`,
		Contains:  []*Mode{numbers, reference},
	}
	node := &Mode{
		ClassName:   "class",
		Begin:       `[a-zA-Z_][a-zA-Z\d_@]*\s\{`,
		End:         `[{;=]`,
		ReturnBegin: true,
		ExcludeEnd:  true,
	}
	rootNode := &Mode{
		ClassName:    "class",
		Begin:        `/\s*\{`,
		End:          `\};`,
		HasRelevance: true,
		Relevance:    10,
		Contains: []*Mode{
			reference,
			keyword,
			label,
			node,
			cellProperty,
			CLineCommentMode,
			CBlockCommentMode,
			numbers,
			stringsMode,
		},
	}

	return &Language{
		Name: "Device Tree",
		Contains: []*Mode{
			rootNode,
			reference,
			keyword,
			label,
			node,
			cellProperty,
			CLineCommentMode,
			CBlockCommentMode,
			numbers,
			stringsMode,
			preprocessor,
			{Begin: IdentRe + `::`, Keywords: Keywords{}},
		},
	}
}
