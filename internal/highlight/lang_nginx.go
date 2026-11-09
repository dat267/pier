package highlight

func init() { RegisterLanguage("nginx", Nginx()) }

// Nginx returns the Nginx config language definition (a port of
// languages/nginx.js).
func Nginx() *Language {
	variable := &Mode{ClassName: "variable"}
	variable.Variants = []*Mode{
		Variant(variable, func(m *Mode) { m.Begin = `\$\d+` }),
		Variant(variable, func(m *Mode) { m.Begin = `\$\{`; m.End = `\}` }),
		// D413: the reference composes this variant from a RegExp and a string,
		// so it never matches and contributes no relevance. The port's working
		// pattern colors the variable but scores nothing, which keeps nginx's
		// auto-detection score equal to the reference's.
		Variant(variable, func(m *Mode) {
			m.Begin = `[$@]` + UnderscoreIdentRe
			m.HasRelevance = true
			m.Relevance = 0
		}),
	}

	strMode := &Mode{ClassName: "string", Contains: []*Mode{BackslashEscape, variable}}
	strMode.Variants = []*Mode{
		Variant(strMode, func(m *Mode) { m.Begin = `"`; m.End = `"` }),
		Variant(strMode, func(m *Mode) { m.Begin = `'`; m.End = `'` }),
	}

	regexpMode := &Mode{ClassName: "regexp", Contains: []*Mode{BackslashEscape, variable}}
	regexpMode.Variants = []*Mode{
		Variant(regexpMode, func(m *Mode) { m.Begin = `\s\^`; m.End = `\s|\{|;`; m.ReturnEnd = true }),
		Variant(regexpMode, func(m *Mode) { m.Begin = `~\*?\s+`; m.End = `\s|\{|;`; m.ReturnEnd = true }),
		Variant(regexpMode, func(m *Mode) { m.Begin = `\*(\.[a-z\-]+)+` }),
		Variant(regexpMode, func(m *Mode) { m.Begin = `([a-z\-]+\.)+\*` }),
	}

	defaultMode := &Mode{
		EndsWithParent: true,
		Keywords: Keywords{
			"$pattern": `[a-z/_]+`,
			"literal": "on off yes no true false none blocked debug info notice warn error crit " +
				"select break last permanent redirect kqueue rtsig epoll poll /dev/poll",
		},
		HasRelevance: true,
		Relevance:    0,
		Illegal:      `=>`,
		Contains: []*Mode{
			HashCommentMode,
			strMode,
			{
				// Swallow entire URLs so their numbers are not detected.
				Begin:          `([a-z]+):/`,
				End:            `\s`,
				EndsWithParent: true,
				ExcludeEnd:     true,
				Contains:       []*Mode{variable},
			},
			regexpMode,
			{ClassName: "number", Begin: `\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(:\d{1,5})?\b`},
			{ClassName: "number", Begin: `\b\d+[kKmMgGdshdwy]*\b`, HasRelevance: true, Relevance: 0},
			variable,
		},
	}

	section := &Mode{
		Begin:        UnderscoreIdentRe + `\s+\{`,
		ReturnBegin:  true,
		End:          `\{`,
		Contains:     []*Mode{{ClassName: "section", Begin: UnderscoreIdentRe}},
		HasRelevance: true,
		Relevance:    0,
	}
	attribute := &Mode{
		Begin:       UnderscoreIdentRe + `\s`,
		End:         `;|\{`,
		ReturnBegin: true,
		Contains: []*Mode{
			{ClassName: "attribute", Begin: UnderscoreIdentRe, Starts: defaultMode},
		},
		HasRelevance: true,
		Relevance:    0,
	}

	return &Language{
		Name:     "Nginx config",
		Aliases:  []string{"nginxconf"},
		Illegal:  `[^\s\}]`,
		Contains: []*Mode{HashCommentMode, section, attribute},
	}
}
