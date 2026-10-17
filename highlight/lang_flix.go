package highlight

func init() { RegisterLanguage("flix", Flix()) }

// Flix returns the Flix language definition (a port of languages/flix.js).
func Flix() *Language {
	char := &Mode{ClassName: "string", Begin: `'(.|\\[xXuU][a-zA-Z0-9]+)'`}

	strBase := &Mode{ClassName: "string"}
	strBase.Variants = []*Mode{
		Variant(strBase, func(m *Mode) { m.Begin = `"`; m.End = `"` }),
	}

	nameBegin := "[^0-9\\n\\t \"'(),.`{}\\[\\]:;][^\\n\\t \"'(),.`{}\\[\\]:;]+|[^0-9\\n\\t \"'(),.`{}\\[\\]:;=]"
	name := &Mode{ClassName: "title", Begin: nameBegin, HasRelevance: true, Relevance: 0}

	method := &Mode{
		ClassName:     "function",
		BeginKeywords: "def",
		End:           `[:={\[(\n;]`,
		ExcludeEnd:    true,
		Contains:      []*Mode{name},
	}

	return &Language{
		Name: "Flix",
		Mode: Mode{
			Keywords: Keywords{
				"literal": "true false",
				"keyword": "case class def else enum if impl import in lat rel index let match namespace switch type yield with",
			},
			Contains: []*Mode{
				CLineCommentMode,
				CBlockCommentMode,
				char,
				strBase,
				method,
				CNumberMode,
			},
		},
	}
}
