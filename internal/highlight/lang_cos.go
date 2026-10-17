package highlight

func init() { RegisterLanguage("cos", COS()) }

// COS returns the Caché Object Script language definition (a port of
// languages/cos.js). Its SQL, JavaScript and XML fragments run as
// sub-languages.
func COS() *Language {
	strBase := &Mode{ClassName: "string"}
	strBase.Variants = []*Mode{
		Variant(strBase, func(m *Mode) {
			m.Begin = `"`
			m.End = `"`
			m.Contains = []*Mode{{Begin: `""`, HasRelevance: true, Relevance: 0}}
		}),
	}

	numbers := &Mode{ClassName: "number", Begin: `\b(\d+(\.\d*)?|\.\d+)`, HasRelevance: true, Relevance: 0}

	bangComment := &Mode{ClassName: "comment", Begin: `;`, End: `$`, HasRelevance: true, Relevance: 0}

	return &Language{
		Name:            "Caché Object Script",
		Aliases:         []string{"cls"},
		CaseInsensitive: true,
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "property parameter class classmethod clientmethod extends as break catch close continue do d|0 else " +
					"elseif for goto halt hang h|0 if job j|0 kill k|0 lock l|0 merge new open quit q|0 read r|0 return " +
					"set s|0 tcommit throw trollback try tstart use view while write w|0 xecute x|0 zkill znspace zn " +
					"ztrap zwrite zw zzdump zzwrite print zbreak zinsert zload zprint zremove zsave zzprint mv mvcall " +
					"mvcrt mvdim mvprint zquit zsync ascii",
			},
			Contains: []*Mode{
				numbers,
				strBase,
				CLineCommentMode,
				CBlockCommentMode,
				bangComment,
				{ClassName: "built_in", Begin: `(?:\$\$?|\.\.)\^?[a-zA-Z]+`},
				{ClassName: "built_in", Begin: `\$\$\$[a-zA-Z]+`},
				{ClassName: "built_in", Begin: `%[a-z]+(?:\.[a-z]+)*`},
				{ClassName: "symbol", Begin: `\^%?[a-zA-Z][\w]*`},
				{ClassName: "keyword", Begin: `##class|##super|#define|#dim`},
				{
					Begin:        `&sql\(`,
					End:          `\)`,
					ExcludeBegin: true,
					ExcludeEnd:   true,
					SubLanguage:  []string{"sql"},
				},
				{
					Begin:        `&(js|jscript|javascript)<`,
					End:          `>`,
					ExcludeBegin: true,
					ExcludeEnd:   true,
					SubLanguage:  []string{"javascript"},
				},
				{Begin: `&html<\s*<`, End: `>\s*>`, SubLanguage: []string{"xml"}},
			},
		},
	}
}
