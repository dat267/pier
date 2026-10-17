package highlight

func init() { RegisterLanguage("apache", Apache()) }

// Apache returns the Apache config language definition (a port of
// languages/apache.js).
func Apache() *Language {
	numberRef := &Mode{ClassName: "number", Begin: `[$%]\d+`}
	number := &Mode{ClassName: "number", Begin: `\d+`}
	ip := &Mode{ClassName: "number", Begin: `\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(:\d{1,5})?`}
	port := &Mode{ClassName: "number", Begin: `:\d{1,5}`}
	quote := Variant(QuoteStringMode, func(m *Mode) { m.HasRelevance = true; m.Relevance = 0 })

	section := &Mode{
		ClassName: "section",
		Begin:     `<\/?`,
		End:       `>`,
		Contains:  []*Mode{ip, port, quote},
	}
	starts := &Mode{
		End:          `$`,
		HasRelevance: true,
		Relevance:    0,
		Keywords:     Keywords{"literal": "on off all deny allow"},
		Contains: []*Mode{
			{ClassName: "meta", Begin: `\s\[`, End: `\]$`},
			{
				ClassName: "variable",
				Begin:     `[\$%]\{`,
				End:       `\}`,
				Contains:  []*Mode{Self, numberRef},
			},
			ip,
			number,
			QuoteStringMode,
		},
	}
	attribute := &Mode{
		ClassName:    "attribute",
		Begin:        `\w+`,
		HasRelevance: true,
		Relevance:    0,
		Keywords: Keywords{
			"nomarkup": "order deny allow setenv rewriterule rewriteengine rewritecond documentroot " +
				"sethandler errordocument loadmodule options header listen serverroot servername",
		},
		Starts: starts,
	}
	return &Language{
		Name:            "Apache config",
		Aliases:         []string{"apacheconf"},
		CaseInsensitive: true,
		Mode: Mode{
			Illegal:  `\S`,
			Contains: []*Mode{HashCommentMode, section, attribute},
		},
	}
}
