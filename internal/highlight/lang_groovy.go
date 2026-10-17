package highlight

func init() { RegisterLanguage("groovy", Groovy()) }

// Groovy returns the Groovy language definition (a port of languages/groovy.js).
func Groovy() *Language {
	const identRe = `[A-Za-z0-9_$]+`

	javadoc := Zero(Mode{
		ClassName: "comment",
		Begin:     `/\*\*`,
		End:       `\*/`,
		Contains: []*Mode{
			Zero(Mode{Begin: `\w+@`}),
			{ClassName: "doctag", Begin: `@[A-Za-z]+`},
			PhrasalWordsMode,
			Zero(Mode{ClassName: "doctag", Begin: `(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):`}),
		},
	})
	comment := &Mode{
		Variants: []*Mode{
			Variant(CLineCommentMode, func(*Mode) {}),
			Variant(CBlockCommentMode, func(*Mode) {}),
			javadoc,
		},
	}
	regexpMode := &Mode{ClassName: "regexp", Begin: `~?\/[^\/\n]+\/`, Contains: []*Mode{BackslashEscape}}
	number := &Mode{
		Variants: []*Mode{
			Variant(BinaryNumberMode, func(*Mode) {}),
			Variant(CNumberMode, func(*Mode) {}),
		},
	}
	stringMode := &Mode{
		ClassName: "string",
		Variants: []*Mode{
			{ClassName: "string", Begin: `"""`, End: `"""`},
			{ClassName: "string", Begin: `'''`, End: `'''`},
			{ClassName: "string", Begin: `\$/`, End: `/\$`, HasRelevance: true, Relevance: 10},
			Variant(AposStringMode, func(*Mode) {}),
			Variant(QuoteStringMode, func(*Mode) {}),
		},
	}

	classMode := &Mode{
		ClassName:     "class",
		BeginKeywords: "class interface trait enum",
		End:           `\{`,
		Illegal:       `:`,
		Contains: []*Mode{
			{BeginKeywords: "extends implements"},
			UnderscoreTitleMode,
		},
	}
	meta := Zero(Mode{ClassName: "meta", Begin: `@[A-Za-z]+`})
	attr := Zero(Mode{ClassName: "attr", Begin: identRe + `[ \t]*:`})
	ternary := Zero(Mode{
		Begin:    `\?`,
		End:      `:`,
		Contains: []*Mode{comment, stringMode, regexpMode, number, Self},
	})
	// The reference uses a `(?=IDENT_RE:)` lookahead begin with excludeBegin; the
	// label text is consumed here instead, which is identical when the label
	// starts at column zero.
	label := Zero(Mode{
		ClassName: "symbol",
		Begin:     `^[ \t]*` + identRe + `:`,
		End:       identRe + `:`,
	})

	return &Language{
		Name: "Groovy",
		Mode: Mode{
			Keywords: Keywords{
				"built_in": "this super",
				"literal":  "true false null",
				"keyword": "byte short char int long boolean float double void " +
					"def as in assert trait " +
					"abstract static volatile transient public private protected synchronized final " +
					"class interface enum if else for while switch case break default continue " +
					"throw throws try catch finally implements extends new import package return instanceof",
			},
			Illegal: `#|<\//`,
			Contains: []*Mode{
				Shebang("groovy", 10),
				comment,
				stringMode,
				regexpMode,
				number,
				classMode,
				meta,
				attr,
				ternary,
				label,
			},
		},
	}
}
