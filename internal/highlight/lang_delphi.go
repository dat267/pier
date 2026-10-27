package highlight

func init() { RegisterLanguage("delphi", Delphi()) }

// Delphi returns the Delphi language definition (a port of languages/delphi.js).
func Delphi() *Language {
	const keywords = "exports register file shl array record property for mod while set ally label uses raise not " +
		"stored class safecall var interface or private static exit index inherited to else stdcall " +
		"override shr asm far resourcestring finalization packed virtual out and protected library do " +
		"xorwrite goto near function end div overload object unit begin string on inline repeat until " +
		"destructor write message program with read initialization except default nil if case cdecl in " +
		"downto threadvar of try pascal const external constructor type public then implementation " +
		"finally published procedure absolute reintroduce operator as is abstract alias assembler " +
		"bitpacked break continue cppdecl cvar enumerator experimental platform deprecated " +
		"unimplemented dynamic export far16 forward generic helper implements interrupt iochecks " +
		"local name nodefault noreturn nostackframe oldfpccall otherwise saveregisters softfloat " +
		"specialize strict unaligned varargs "

	commentBrace := CommentMode(`\{`, `\}`)
	commentBrace.HasRelevance = true
	commentBrace.Relevance = 0
	commentParen := CommentMode(`\(\*`, `\*\)`)
	commentParen.HasRelevance = true
	commentParen.Relevance = 10
	commentModes := []*Mode{CLineCommentMode, commentBrace, commentParen}

	directive := &Mode{ClassName: "meta"}
	directive.Variants = []*Mode{
		Variant(directive, func(m *Mode) { m.Begin = `\{\$`; m.End = `\}` }),
		Variant(directive, func(m *Mode) { m.Begin = `\(\*\$`; m.End = `\*\)` }),
	}

	str := &Mode{ClassName: "string", Begin: `'`, End: `'`, Contains: []*Mode{{Begin: `''`}}}

	number := &Mode{ClassName: "number", HasRelevance: true, Relevance: 0}
	number.Variants = []*Mode{
		Variant(number, func(m *Mode) { m.Begin = `\$[0-9A-Fa-f]+` }),
		Variant(number, func(m *Mode) { m.Begin = `&[0-7]+` }),
		Variant(number, func(m *Mode) { m.Begin = `%[01]+` }),
	}

	charString := &Mode{ClassName: "string", Begin: `(#\d+)+`}

	classMode := &Mode{
		Begin:       IdentRe + `\s*=\s*class\s*\(`,
		ReturnBegin: true,
		Contains:    []*Mode{TitleMode},
	}

	function := &Mode{
		ClassName:     "function",
		BeginKeywords: "function constructor destructor procedure",
		End:           `[:;]`,
		Keywords:      Keywords{"keyword": "function constructor|10 destructor|10 procedure|10"},
		Contains: []*Mode{
			TitleMode,
			{
				ClassName: "params",
				Begin:     `\(`,
				End:       `\)`,
				Keywords:  Keywords{"keyword": keywords},
				Contains:  append([]*Mode{str, charString, directive}, commentModes...),
			},
			directive,
		},
	}

	return &Language{
		Name: "Delphi",
		Aliases: []string{
			"dpr", "dfm", "pas", "pascal", "freepascal", "lazarus", "lpr", "lfm",
		},
		CaseInsensitive: true,
		Keywords:        Keywords{"keyword": keywords},
		Illegal:         `"|\$[G-Zg-z]|\/\*|<\/|\|`,
		Contains: append([]*Mode{
			str,
			charString,
			NumberMode,
			number,
			classMode,
			function,
			directive,
		}, commentModes...),
	}
}
