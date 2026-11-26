package highlight

import "unicode/utf8"

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
	// The reference's label rule is `begin: '^[ \t]*' + lookahead(IDENT_RE + ':')`
	// with excludeBegin and `end: IDENT_RE + ':'`: the begin is the INDENT, which
	// the lookahead keeps out of the symbol, and the symbol span is only the
	// `name:` the end matches. Consuming the label in the begin instead made the
	// begin swallow the indentation and let the end run on to the NEXT label, so
	// two indented labels merged into one multi-line symbol.
	label := Zero(Mode{
		ClassName:       "symbol",
		ManualBegin:     groovyLabelBegin,
		ManualFirst:     " \t",
		ManualLineStart: true,
		ExcludeBegin:    true,
		End:             identRe + `:`,
	})

	return &Language{
		Name: "Groovy",
		Keywords: Keywords{
			"built_in": "this super",
			"literal":  "true false null",
			"keyword": "byte short char int long boolean float double void " +
				"def as in assert trait " +
				"abstract static volatile transient public private protected synchronized final " +
				"class interface enum if else for while switch case break default continue " +
				"throw throws try catch finally implements extends new import package return instanceof",
		},
		Illegal: `#|<\/`, // the reference is /#|<\// (close-tag), not a literal "<//"
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
	}
}

// groovyLabelBegin ports `^[ \t]*(?=IDENT_RE:)` with excludeBegin: it matches at a
// line start only, and ends at the label so the indentation stays plain text.
func groovyLabelBegin(s string, pos int) (int, bool) {
	if pos > 0 {
		previous, _ := utf8.DecodeLastRuneInString(s[:pos])
		if !jsLineEnd(previous) {
			return 0, false
		}
	}
	at := pos
	for at < len(s) && (s[at] == ' ' || s[at] == '\t') {
		at++
	}
	// IDENT_RE is `[A-Za-z0-9_$]+`, and the lookahead also requires the colon.
	end := at
	for end < len(s) && isGroovyIdentByte(s[end]) {
		end++
	}
	if end == at || end >= len(s) || s[end] != ':' {
		return 0, false
	}
	return at, true
}

func isGroovyIdentByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	}
	return b == '_' || b == '$'
}
