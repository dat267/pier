package highlight

func init() { RegisterLanguage("moonscript", MoonScript()) }

// MoonScript returns the MoonScript language definition (a port of
// languages/moonscript.js).
func MoonScript() *Language {
	keywords := Keywords{
		"keyword": "if then not for in while do return else elseif break continue switch and or unless when class " +
			"extends super local import export from using",
		"literal": "true false nil",
		"built_in": "_G _VERSION assert collectgarbage dofile error getfenv getmetatable ipairs load loadfile loadstring " +
			"module next pairs pcall print rawequal rawget rawset require select setfenv setmetatable tonumber " +
			"tostring type unpack xpcall coroutine debug io math os package string table",
	}

	const jsIdentRe = `[A-Za-z$_][0-9A-Za-z$_]*`
	const possibleParamsRe = `(\(.*\)\s*)?\B[-=]>`

	subst := &Mode{ClassName: "subst", Begin: `#\{`, End: `\}`, Keywords: keywords}

	number := Variant(CNumberMode, func(m *Mode) {
		m.Starts = &Mode{End: `(\s*/)?`, HasRelevance: true, Relevance: 0}
	})
	stringMode := &Mode{ClassName: "string"}
	stringMode.Variants = []*Mode{
		Variant(stringMode, func(m *Mode) { m.Begin = `'`; m.End = `'`; m.Contains = []*Mode{BackslashEscape} }),
		Variant(stringMode, func(m *Mode) { m.Begin = `"`; m.End = `"`; m.Contains = []*Mode{BackslashEscape, subst} }),
	}

	expressions := []*Mode{
		number,
		stringMode,
		{ClassName: "built_in", Begin: `@__` + IdentRe},
		{Begin: `@` + IdentRe},
		{Begin: IdentRe + `\\` + IdentRe},
	}
	subst.Contains = expressions

	title := Variant(TitleMode, func(m *Mode) { m.Begin = jsIdentRe })
	params := &Mode{
		ClassName:   "params",
		Begin:       `\([^\(]`,
		ReturnBegin: true,
		Contains: []*Mode{
			{
				Begin:    `\(`,
				End:      `\)`,
				Keywords: keywords,
				Contains: append([]*Mode{Self}, expressions...),
			},
		},
	}

	function := &Mode{
		ClassName:   "function",
		Begin:       `^\s*` + jsIdentRe + `\s*=\s*` + possibleParamsRe,
		End:         `[-=]>`,
		ReturnBegin: true,
		Contains:    []*Mode{title, params},
	}
	anonymous := &Mode{
		Begin:        `[\(,:=]\s*`,
		HasRelevance: true,
		Relevance:    0,
		Contains: []*Mode{
			{
				ClassName:   "function",
				Begin:       possibleParamsRe,
				End:         `[-=]>`,
				ReturnBegin: true,
				Contains:    []*Mode{params},
			},
		},
	}
	classMode := &Mode{
		ClassName:     "class",
		BeginKeywords: "class",
		End:           `$`,
		Illegal:       `[:="\[\]]`,
		Contains: []*Mode{
			{
				BeginKeywords:  "extends",
				EndsWithParent: true,
				Illegal:        `[:="\[\]]`,
				Contains:       []*Mode{title},
			},
			title,
		},
	}
	nameMode := &Mode{
		ClassName:    "name",
		Begin:        jsIdentRe + `:`,
		End:          `:`,
		ReturnBegin:  true,
		ReturnEnd:    true,
		HasRelevance: true,
		Relevance:    0,
	}

	contains := append([]*Mode{}, expressions...)
	contains = append(contains, CommentMode(`--`, `$`), function, anonymous, classMode, nameMode)

	return &Language{
		Name:    "MoonScript",
		Aliases: []string{"moon"},
		Mode: Mode{
			Keywords: keywords,
			Illegal:  `/\*`,
			Contains: contains,
		},
	}
}
