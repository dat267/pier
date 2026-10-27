package highlight

func init() { RegisterLanguage("capnproto", CapNProto()) }

// CapNProto returns the Cap'n Proto language definition (a port of
// languages/capnproto.js).
func CapNProto() *Language {
	title := Variant(TitleMode, func(m *Mode) {
		m.Starts = &Mode{EndsWithParent: true, ExcludeEnd: true}
	})
	return &Language{
		Name:    "Cap\u2019n Proto",
		Aliases: []string{"capnp"},
		Keywords: Keywords{
			"keyword": "struct enum interface union group import using const annotation extends in of on as with from fixed",
			"built_in": "Void Bool Int8 Int16 Int32 Int64 UInt8 UInt16 UInt32 UInt64 Float32 Float64 Text Data AnyPointer " +
				"AnyStruct Capability List",
			"literal": "true false",
		},
		Contains: []*Mode{
			QuoteStringMode,
			NumberMode,
			HashCommentMode,
			{ClassName: "meta", Begin: `@0x[\w\d]{16};`, Illegal: `\n`},
			{ClassName: "symbol", Begin: `@\d+\b`},
			{ClassName: "class", BeginKeywords: "struct enum", End: `\{`, Illegal: `\n`, Contains: []*Mode{title}},
			{ClassName: "class", BeginKeywords: "interface", End: `\{`, Illegal: `\n`, Contains: []*Mode{title}},
		},
	}
}
