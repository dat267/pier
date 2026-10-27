package highlight

import "regexp"

func init() { RegisterLanguage("protobuf", Protobuf()) }

// protobufEnumGuard is the reference's `(?=\s*=[^\n]+;$)` lookahead on the
// enum-item relevance rule.
var protobufEnumGuard = regexp.MustCompile(`\s*=[^\n]+;$`)

// Protobuf returns the Protocol Buffers language definition (a port of
// languages/protobuf.js).
func Protobuf() *Language {
	title := Variant(TitleMode, func(m *Mode) {
		// Eat everything after the first title.
		m.Starts = &Mode{EndsWithParent: true, ExcludeEnd: true}
	})

	enumItem := &Mode{Begin: `^\s*[A-Z_]+`}
	enumItem.beforeBegin = func(m *match, resp *Response) {
		if !startsWith(protobufEnumGuard, m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}

	return &Language{
		Name: "Protocol Buffers",
		Keywords: Keywords{
			"keyword":  "package import option optional required repeated group oneof",
			"built_in": "double float int32 int64 uint32 uint64 sint32 sint64 fixed32 fixed64 sfixed32 sfixed64 bool string bytes",
			"literal":  "true false",
		},
		Contains: []*Mode{
			QuoteStringMode,
			NumberMode,
			CLineCommentMode,
			CBlockCommentMode,
			{
				ClassName:     "class",
				BeginKeywords: "message enum service",
				End:           `\{`,
				Illegal:       `\n`,
				Contains:      []*Mode{title},
			},
			{
				ClassName:     "function",
				BeginKeywords: "rpc",
				End:           `[{;]`,
				ExcludeEnd:    true,
				Keywords:      Keywords{"keyword": "rpc returns"},
			},
			enumItem,
		},
	}
}
