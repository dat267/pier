package highlight

func init() { RegisterLanguage("cal", CAL()) }

// CAL returns the C/AL language definition (a port of languages/cal.js).
func CAL() *Language {
	keyword := "div mod in and or not xor asserterror begin case do downto else end exit for if of repeat then to " +
		"until while with var"

	commentBrace := CommentMode(`\{`, `\}`)
	commentBrace.HasRelevance = true
	commentBrace.Relevance = 0
	commentParen := CommentMode(`\(\*`, `\*\)`)
	commentParen.HasRelevance = true
	commentParen.Relevance = 10
	commentModes := []*Mode{CLineCommentMode, commentBrace, commentParen}

	str := &Mode{ClassName: "string", Begin: `'`, End: `'`, Contains: []*Mode{{Begin: `''`}}}
	charString := &Mode{ClassName: "string", Begin: `(#\d+)+`}
	date := &Mode{ClassName: "number", Begin: `\b\d+(\.\d+)?(DT|D|T)`, HasRelevance: true, Relevance: 0}
	dblQuoted := &Mode{ClassName: "string", Begin: `"`, End: `"`}

	procedure := &Mode{
		ClassName:     "function",
		BeginKeywords: "procedure",
		End:           `[:;]`,
		Keywords:      Keywords{"keyword": "procedure|10"},
		Contains: append([]*Mode{
			TitleMode,
			{
				ClassName: "params",
				Begin:     `\(`,
				End:       `\)`,
				Keywords:  Keywords{"keyword": keyword},
				Contains:  []*Mode{str, charString},
			},
		}, commentModes...),
	}

	object := &Mode{
		ClassName:   "class",
		Begin:       `OBJECT (Table|Form|Report|Dataport|Codeunit|XMLport|MenuSuite|Page|Query) (\d+) ([^\r\n]+)`,
		ReturnBegin: true,
		Contains:    []*Mode{TitleMode, procedure},
	}

	return &Language{
		Name:            "C/AL",
		CaseInsensitive: true,
		Keywords:        Keywords{"keyword": keyword, "literal": "false true"},
		Illegal:         `/\*`,
		Contains: []*Mode{
			str,
			charString,
			date,
			dblQuoted,
			NumberMode,
			object,
			procedure,
		},
	}
}
