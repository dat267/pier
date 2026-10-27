package highlight

func init() { RegisterLanguage("zephir", Zephir()) }

// Zephir returns the Zephir language definition (a port of languages/zephir.js).
func Zephir() *Language {
	strBase := &Mode{ClassName: "string", Contains: []*Mode{BackslashEscape}}
	strBase.Variants = []*Mode{
		Variant(strBase, func(m *Mode) { m.Begin = `'`; m.End = `'`; m.Illegal = "" }),
		Variant(strBase, func(m *Mode) { m.Begin = `"`; m.End = `"`; m.Illegal = "" }),
	}
	number := &Mode{Variants: []*Mode{BinaryNumberMode, CNumberMode}}

	blockComment := &Mode{
		ClassName: "comment",
		Begin:     `/\*`,
		End:       `\*/`,
		Contains:  []*Mode{{ClassName: "doctag", Begin: `@[A-Za-z]+`}},
	}

	heredoc := &Mode{
		ClassName: "string",
		Begin:     `<<<['"]?\w+['"]?$`,
		End:       `^\w+;`,
		Contains:  []*Mode{BackslashEscape},
	}

	keywords := Keywords{
		"keyword": "namespace class interface use extends function return abstract final public protected private static " +
			"deprecated throw try catch Exception echo empty isset instanceof unset let var new const self " +
			"require if else elseif switch case default do while loop for continue break likely unlikely __LINE__ " +
			"__FILE__ __DIR__ __FUNCTION__ __CLASS__ __TRAIT__ __METHOD__ __NAMESPACE__ array boolean float " +
			"double integer object resource string char long unsigned bool int uint ulong uchar true false null " +
			"undefined",
	}

	function := &Mode{
		ClassName:     "function",
		BeginKeywords: "function fn",
		End:           `[;{]`,
		ExcludeEnd:    true,
		Illegal:       `\$|\[|%`,
		Contains: []*Mode{
			UnderscoreTitleMode,
			{
				ClassName: "params",
				Begin:     `\(`,
				End:       `\)`,
				Keywords:  keywords,
				Contains:  []*Mode{Self, CBlockCommentMode, strBase, number},
			},
		},
	}
	classMode := &Mode{
		ClassName:     "class",
		BeginKeywords: "class interface",
		End:           `\{`,
		ExcludeEnd:    true,
		Illegal:       `[:($"]`,
		Contains: []*Mode{
			{BeginKeywords: "extends implements"},
			UnderscoreTitleMode,
		},
	}

	return &Language{
		Name:     "Zephir",
		Aliases:  []string{"zep"},
		Keywords: keywords,
		Contains: []*Mode{
			CLineCommentMode,
			blockComment,
			heredoc,
			{Begin: `(::|->)+[a-zA-Z_\x7f-\xff][a-zA-Z0-9_\x7f-\xff]*`},
			function,
			classMode,
			{
				BeginKeywords: "namespace",
				End:           `;`,
				Illegal:       `[.']`,
				Contains:      []*Mode{UnderscoreTitleMode},
			},
			{
				BeginKeywords: "use",
				End:           `;`,
				Contains:      []*Mode{UnderscoreTitleMode},
			},
			{Begin: `=>`},
			strBase,
			number,
		},
	}
}
