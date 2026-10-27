package highlight

func init() { RegisterLanguage("go", GoLanguage()) }

// GoLanguage returns the Go language definition (a port of languages/go.js).
func GoLanguage() *Language {
	keywords := Keywords{
		"keyword": "break default func interface select case map struct chan else goto package switch " +
			"const fallthrough if range type continue for import return var go defer " +
			"bool byte complex64 complex128 float32 float64 int8 int16 int32 int64 string uint8 " +
			"uint16 uint32 uint64 int uint uintptr rune",
		"literal":  "true false iota nil",
		"built_in": "append cap close complex copy imag len make new panic print println real recover delete",
	}
	return &Language{
		Name:     "Go",
		Aliases:  []string{"golang"},
		Keywords: keywords,
		Illegal:  `</`,
		Contains: []*Mode{
			CLineCommentMode,
			CBlockCommentMode,
			{
				ClassName: "string",
				Variants: []*Mode{
					Variant(QuoteStringMode, func(*Mode) {}),
					Variant(AposStringMode, func(*Mode) {}),
					{ClassName: "string", Begin: "`", End: "`"},
				},
			},
			{
				ClassName: "number",
				Variants: []*Mode{
					Variant(CNumberMode, func(m *Mode) { m.Begin = CNumberRe + `[i]`; m.Relevance = 1 }),
					Variant(CNumberMode, func(*Mode) {}),
				},
			},
			{Begin: `:=`},
			{
				ClassName:     "function",
				BeginKeywords: "func",
				End:           `\s*(\{|$)`,
				ExcludeEnd:    true,
				Contains: []*Mode{
					TitleMode,
					{
						ClassName: "params",
						Begin:     `\(`,
						End:       `\)`,
						Keywords:  keywords,
						Illegal:   `["']`,
					},
				},
			},
		},
	}
}
