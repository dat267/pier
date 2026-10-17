package highlight

import "regexp"

func init() { RegisterLanguage("ini", INI()) }

// iniKeyGuard is the reference's `(?=\s*=\s*[^#\s])` lookahead on the dotted key.
var iniKeyGuard = regexp.MustCompile(`\A\s*=\s*[^#\s]`)

// INI returns the TOML/INI language definition (a port of languages/ini.js).
func INI() *Language {
	comments := &Mode{
		ClassName: "comment",
		Contains: []*Mode{
			PhrasalWordsMode,
			Zero(Mode{ClassName: "doctag", Begin: `(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):`}),
		},
	}
	comments.Variants = []*Mode{
		Variant(comments, func(m *Mode) { m.Begin = `;`; m.End = `$` }),
		Variant(comments, func(m *Mode) { m.Begin = `#`; m.End = `$` }),
	}

	numbers := &Mode{ClassName: "number", HasRelevance: true, Relevance: 0}
	numbers.Variants = []*Mode{
		Variant(numbers, func(m *Mode) { m.Begin = `([+-]+)?[\d]+_[\d_]+` }),
		Variant(numbers, func(m *Mode) { m.Begin = NumberRe }),
	}

	variables := &Mode{ClassName: "variable"}
	variables.Variants = []*Mode{
		Variant(variables, func(m *Mode) { m.Begin = `\$[\w\d"][\w\d_]*` }),
		Variant(variables, func(m *Mode) { m.Begin = `\$\{(.*?)\}` }),
	}

	literals := &Mode{ClassName: "literal", Begin: `\bon|off|true|false|yes|no\b`}

	strings := &Mode{ClassName: "string", Contains: []*Mode{BackslashEscape}}
	strings.Variants = []*Mode{
		Variant(strings, func(m *Mode) { m.Begin = `'''`; m.End = `'''`; m.HasRelevance = true; m.Relevance = 10 }),
		Variant(strings, func(m *Mode) { m.Begin = `"""`; m.End = `"""`; m.HasRelevance = true; m.Relevance = 10 }),
		Variant(strings, func(m *Mode) { m.Begin = `"`; m.End = `"` }),
		Variant(strings, func(m *Mode) { m.Begin = `'`; m.End = `'` }),
	}

	array := &Mode{
		Begin:        `\[`,
		End:          `\]`,
		HasRelevance: true,
		Relevance:    0,
		Contains:     []*Mode{comments, literals, variables, strings, numbers, Self},
	}

	const anyKey = `([A-Za-z0-9_-]+|"(\\"|[^"])*"|'[^']*')`
	dottedKey := &Mode{
		ClassName: "attr",
		Begin:     anyKey + `(\s*\.\s*` + anyKey + `)*`,
		Starts: &Mode{
			End:      `$`,
			Contains: []*Mode{comments, array, literals, variables, strings, numbers},
		},
	}
	dottedKey.beforeBegin = func(m *match, resp *Response) {
		if !iniKeyGuard.MatchString(m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}

	return &Language{
		Name:            "TOML, also INI",
		Aliases:         []string{"toml"},
		CaseInsensitive: true,
		Mode: Mode{
			Illegal: `\S`,
			Contains: []*Mode{
				comments,
				{ClassName: "section", Begin: `\[+`, End: `\]+`},
				dottedKey,
			},
		},
	}
}
