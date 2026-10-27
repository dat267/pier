package highlight

func init() { RegisterLanguage("thrift", Thrift()) }

// Thrift returns the Thrift language definition (a port of languages/thrift.js).
// Its container keywords use the default keyword class, not built_in.
func Thrift() *Language {
	title := Variant(TitleMode, func(m *Mode) {
		// Eat everything after the first title.
		m.Starts = &Mode{EndsWithParent: true, ExcludeEnd: true}
	})
	const builtInTypes = "bool byte i16 i32 i64 double string binary"
	return &Language{
		Name: "Thrift",
		Keywords: Keywords{
			"keyword":  "namespace const typedef struct enum service exception void oneway set list map required optional",
			"built_in": builtInTypes,
			"literal":  "true false",
		},
		Contains: []*Mode{
			QuoteStringMode,
			NumberMode,
			CLineCommentMode,
			CBlockCommentMode,
			{
				ClassName:     "class",
				BeginKeywords: "struct enum service exception",
				End:           `\{`,
				Illegal:       `\n`,
				Contains:      []*Mode{title},
			},
			{
				Begin:    `\b(set|list|map)\s*<`,
				End:      `>`,
				Keywords: Keywords{"keyword": builtInTypes},
				Contains: []*Mode{Self},
			},
		},
	}
}
