package highlight

import "regexp"

func init() { RegisterLanguage("makefile", Makefile()) }

// makefileAssignGuard is the reference's `(?=[:+?]?=)` lookahead: RE2 has none,
// so the assignment rule matches the target and checks the following bytes.
var makefileAssignGuard = regexp.MustCompile(`[:+?]?=`)

// Makefile returns the Makefile language definition (a port of
// languages/makefile.js).
func Makefile() *Language {
	variable := &Mode{ClassName: "variable"}
	variable.Variants = []*Mode{
		Variant(variable, func(m *Mode) {
			m.Begin = `\$\(` + UnderscoreIdentRe + `\)`
			m.Contains = []*Mode{BackslashEscape}
		}),
		Variant(variable, func(m *Mode) { m.Begin = `\$[@%<?\^\+\*]` }),
	}

	quoteString := &Mode{
		ClassName: "string",
		Begin:     `"`,
		End:       `"`,
		Contains:  []*Mode{BackslashEscape, variable},
	}

	funcMode := &Mode{
		ClassName: "variable",
		Begin:     `\$\([\w-]+\s`,
		End:       `\)`,
		Keywords: Keywords{
			"built_in": "subst patsubst strip findstring filter filter-out sort " +
				"word wordlist firstword lastword dir notdir suffix basename " +
				"addsuffix addprefix join wildcard realpath abspath error warning " +
				"shell origin flavor foreach if or and call eval file value",
		},
		Contains: []*Mode{variable},
	}

	assignment := &Mode{Begin: `^` + UnderscoreIdentRe + `\s*`}
	assignment.beforeBegin = func(m *match, resp *Response) {
		if !startsWith(makefileAssignGuard, m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}

	meta := &Mode{
		ClassName: "meta",
		Begin:     `^\.PHONY:`,
		End:       `$`,
		Keywords:  Keywords{"$pattern": `[\.\w]+`, "meta-keyword": ".PHONY"},
	}

	target := &Mode{
		ClassName: "section",
		Begin:     `^[^\s]+:`,
		End:       `$`,
		Contains:  []*Mode{variable},
	}

	return &Language{
		Name:    "Makefile",
		Aliases: []string{"mk", "mak", "make"},
		Keywords: Keywords{
			"$pattern": `[\w-]+`,
			"keyword": "define endef undefine ifdef ifndef ifeq ifneq else endif " +
				"include -include sinclude override export unexport private vpath",
		},
		Contains: []*Mode{
			HashCommentMode,
			variable,
			quoteString,
			funcMode,
			assignment,
			meta,
			target,
		},
	}
}
