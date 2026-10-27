package highlight

func init() { RegisterLanguage("axapta", Axapta()) }

// Axapta returns the Microsoft X++ language definition (a port of
// languages/axapta.js).
func Axapta() *Language {
	return &Language{
		Name:    "X++",
		Aliases: []string{"x++"},
		Keywords: Keywords{
			"keyword": "abstract as asc avg break breakpoint by byref case catch changecompany class client client common " +
				"const continue count crosscompany delegate delete_from desc display div do edit else eventhandler " +
				"exists extends final finally firstfast firstonly firstonly1 firstonly10 firstonly100 firstonly1000 " +
				"flush for forceliterals forcenestedloop forceplaceholders forceselectorder forupdate from " +
				"generateonly group hint if implements in index insert_recordset interface internal is join like " +
				"maxof minof mod namespace new next nofetch notexists optimisticlock order outer pessimisticlock " +
				"print private protected public readonly repeatableread retry return reverse select server setting " +
				"static sum super switch this throw try ttsabort ttsbegin ttscommit unchecked update_recordset using " +
				"validtimestate void where while",
			"built_in": "anytype boolean byte char container date double enum guid int int64 long real short str utcdatetime " +
				"var",
			"literal": "default false null true",
		},
		Contains: []*Mode{
			CLineCommentMode,
			CBlockCommentMode,
			AposStringMode,
			QuoteStringMode,
			CNumberMode,
			{ClassName: "meta", Begin: `#`, End: `$`},
			{
				ClassName:     "class",
				BeginKeywords: "class interface",
				End:           `\{`,
				ExcludeEnd:    true,
				Illegal:       `:`,
				Contains: []*Mode{
					{BeginKeywords: "extends implements"},
					UnderscoreTitleMode,
				},
			},
		},
	}
}
