package highlight

func init() { RegisterLanguage("angelscript", AngelScript()) }

// AngelScript returns the AngelScript language definition (a port of
// languages/angelscript.js). The generic rule is reached from both the
// built-in-type and object-handle rules, so they reference each other.
func AngelScript() *Language {
	builtInType := &Mode{
		ClassName: "built_in",
		Begin:     `\b(void|bool|int|int8|int16|int32|int64|uint|uint8|uint16|uint32|uint64|string|ref|array|double|float|auto|dictionary)`,
	}
	objectHandle := &Mode{ClassName: "symbol", Begin: `[a-zA-Z0-9_]+@`}
	generic := &Mode{
		ClassName: "keyword",
		Begin:     `<`,
		End:       `>`,
		Contains:  []*Mode{builtInType, objectHandle},
	}
	builtInType.Contains = []*Mode{generic}
	objectHandle.Contains = []*Mode{generic}

	return &Language{
		Name:    "AngelScript",
		Aliases: []string{"asc"},
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "for in|0 break continue while do|0 return if else case switch namespace is cast " +
					"or and xor not get|0 in inout|10 out override set|0 private public const default|0 " +
					"final shared external mixin|10 enum typedef funcdef this super import from interface " +
					"abstract|0 try catch protected explicit property",
			},
			Illegal: `(^using\s+[A-Za-z0-9_\.]+;$|\bfunction\s*[^\(])`,
			Contains: []*Mode{
				{
					ClassName:    "string",
					Begin:        `'`,
					End:          `'`,
					Illegal:      `\n`,
					Contains:     []*Mode{BackslashEscape},
					HasRelevance: true,
					Relevance:    0,
				},
				{ClassName: "string", Begin: `"""`, End: `"""`},
				{
					ClassName:    "string",
					Begin:        `"`,
					End:          `"`,
					Illegal:      `\n`,
					Contains:     []*Mode{BackslashEscape},
					HasRelevance: true,
					Relevance:    0,
				},
				CLineCommentMode,
				CBlockCommentMode,
				{ClassName: "string", Begin: `^\s*\[`, End: `\]`},
				{
					BeginKeywords: "interface namespace",
					End:           `\{`,
					Illegal:       `[;.\-]`,
					Contains:      []*Mode{{ClassName: "symbol", Begin: `[a-zA-Z0-9_]+`}},
				},
				{
					BeginKeywords: "class",
					End:           `\{`,
					Illegal:       `[;.\-]`,
					Contains: []*Mode{
						{
							ClassName: "symbol",
							Begin:     `[a-zA-Z0-9_]+`,
							Contains: []*Mode{
								{
									Begin:    `[:,]\s*`,
									Contains: []*Mode{{ClassName: "symbol", Begin: `[a-zA-Z0-9_]+`}},
								},
							},
						},
					},
				},
				builtInType,
				objectHandle,
				{ClassName: "literal", Begin: `\b(null|true|false)`},
				{
					ClassName:    "number",
					Begin:        `(-?)(\b0[xXbBoOdD][a-fA-F0-9]+|(\b\d+(\.\d*)?f?|\.\d+f?)([eE][-+]?\d+f?)?)`,
					HasRelevance: true,
					Relevance:    0,
				},
			},
		},
	}
}
