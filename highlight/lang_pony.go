package highlight

func init() { RegisterLanguage("pony", Pony()) }

// Pony returns the Pony language definition (a port of languages/pony.js).
func Pony() *Language {
	return &Language{
		Name: "Pony",
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "actor addressof and as be break class compile_error compile_intrinsic consume continue delegate " +
					"digestof do else elseif embed end error for fun if ifdef in interface is isnt lambda let match new " +
					"not object or primitive recover repeat return struct then trait try type until use var where while " +
					"with xor",
				"meta":    "iso val tag trn box ref",
				"literal": "this false true",
			},
			Contains: []*Mode{
				{ClassName: "type", Begin: `\b_?[A-Z][\w]*`, HasRelevance: true, Relevance: 0},
				{ClassName: "string", Begin: `"""`, End: `"""`, HasRelevance: true, Relevance: 10},
				{ClassName: "string", Begin: `"`, End: `"`, Contains: []*Mode{BackslashEscape}},
				{
					ClassName:    "string",
					Begin:        `'`,
					End:          `'`,
					Contains:     []*Mode{BackslashEscape},
					HasRelevance: true,
					Relevance:    0,
				},
				{Begin: IdentRe + `'`, HasRelevance: true, Relevance: 0},
				{
					ClassName:    "number",
					Begin:        `(-?)(\b0[xX][a-fA-F0-9]+|\b0[bB][01]+|(\b\d+(_\d+)?(\.\d*)?|\.\d+)([eE][-+]?\d+)?)`,
					HasRelevance: true,
					Relevance:    0,
				},
				CLineCommentMode,
				CBlockCommentMode,
			},
		},
	}
}
