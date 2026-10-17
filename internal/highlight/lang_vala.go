package highlight

func init() { RegisterLanguage("vala", Vala()) }

// Vala returns the Vala language definition (a port of languages/vala.js).
func Vala() *Language {
	return &Language{
		Name: "Vala",
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "char uchar unichar int uint long ulong short ushort int8 int16 int32 int64 uint8 " +
					"uint16 uint32 uint64 float double bool struct enum string void " +
					"weak unowned owned " +
					"async signal static abstract interface override virtual delegate " +
					"if while do for foreach else switch case break default return try catch " +
					"public private protected internal " +
					"using new this get set const stdout stdin stderr var",
				"built_in": "DBus GLib CCode Gee Object Gtk Posix",
				"literal":  "false true null",
			},
			Contains: []*Mode{
				{
					ClassName:     "class",
					BeginKeywords: "class interface namespace",
					End:           `\{`,
					ExcludeEnd:    true,
					Illegal:       `[^,:\n\s\.]`,
					Contains:      []*Mode{UnderscoreTitleMode},
				},
				CLineCommentMode,
				CBlockCommentMode,
				{ClassName: "string", Begin: `"""`, End: `"""`, HasRelevance: true, Relevance: 5},
				AposStringMode,
				QuoteStringMode,
				CNumberMode,
				{ClassName: "meta", Begin: `^#`, End: `$`, HasRelevance: true, Relevance: 2},
			},
		},
	}
}
