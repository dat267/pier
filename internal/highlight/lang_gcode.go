package highlight

func init() { RegisterLanguage("gcode", GCode()) }

// GCode returns the G-code (ISO 6983) language definition (a port of
// languages/gcode.js).
func GCode() *Language {
	numbers := Variant(CNumberMode, func(m *Mode) {
		m.Begin = `([-+]?((\.\d+)|(\d+)(\.\d*)?))|` + CNumberRe
	})
	clearIllegal := func(m *Mode) { m.Illegal = "" }
	symbolBase := &Mode{ClassName: "symbol"}
	symbolBase.Variants = []*Mode{
		Variant(symbolBase, func(m *Mode) { m.Begin = `N`; m.End = `\d+`; m.Illegal = `\W` }),
	}
	code := []*Mode{
		CLineCommentMode,
		CBlockCommentMode,
		CommentMode(`\(`, `\)`),
		numbers,
		Variant(AposStringMode, clearIllegal),
		Variant(QuoteStringMode, clearIllegal),
		{ClassName: "name", Begin: `([G])([0-9]+\.?[0-9]?)`},
		{ClassName: "name", Begin: `([M])([0-9]+\.?[0-9]?)`},
		{ClassName: "attr", Begin: `(VC|VS|#)`, End: `(\d+)`},
		{ClassName: "attr", Begin: `(VZOFX|VZOFY|VZOFZ)`},
		{
			ClassName: "built_in",
			Begin:     `(ATAN|ABS|ACOS|ASIN|SIN|COS|EXP|FIX|FUP|ROUND|LN|TAN)(\[)`,
			Contains:  []*Mode{numbers},
			End:       `\]`,
		},
		symbolBase,
	}
	contains := []*Mode{
		{ClassName: "meta", Begin: `%`},
		{ClassName: "meta", Begin: `([O])([0-9]+)`},
	}
	contains = append(contains, code...)
	return &Language{
		Name:            "G-code (ISO 6983)",
		Aliases:         []string{"nc"},
		CaseInsensitive: true,
		Mode: Mode{
			Keywords: Keywords{
				"$pattern": "[A-Z_][A-Z0-9_.]*",
				"keyword":  "IF DO WHILE ENDWHILE CALL ENDIF SUB ENDSUB GOTO REPEAT ENDREPEAT EQ LT GT NE GE LE OR XOR",
			},
			Contains: contains,
		},
	}
}
