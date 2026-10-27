package highlight

func init() { RegisterLanguage("c", C()) }

func optionalRe(s string) string { return "(" + s + ")?" }

// C returns the C language definition (a port of languages/c.js). It is also
// the base for C++.
func C() *Language {
	lineComment := commentMode(`//`, `$`)
	lineComment.Contains = append(lineComment.Contains, &Mode{Begin: `\\\n`})

	const decltypeAutoRe = `decltype\(auto\)`
	const namespaceRe = `[a-zA-Z_]\w*::`
	const templateArgumentRe = `<[^<>]+>`
	functionTypeRe := "(" + decltypeAutoRe + "|" + optionalRe(namespaceRe) + `[a-zA-Z_]\w*` + optionalRe(templateArgumentRe) + ")"

	cppPrimitiveTypes := &Mode{ClassName: "keyword", Begin: `\b[a-z\d_]*_t\b`}

	const characterEscapes = `\\(x[0-9A-Fa-f]{2}|u[0-9A-Fa-f]{4,8}|[0-7]{3}|\S)`
	strings := &Mode{
		ClassName: "string",
		Variants: []*Mode{
			{ClassName: "string", Begin: `(u8?|U|L)?"`, End: `"`, Illegal: `\n`, Contains: []*Mode{BackslashEscape}},
			{ClassName: "string", Begin: `(u8?|U|L)?'(` + characterEscapes + `|.)`, End: `'`, Illegal: `.`},
			EndSameAsBegin(&Mode{ClassName: "string", Begin: `(?:u8?|U|L)?R"([^()\\ ]{0,16})\(`, End: `\)([^()\\ ]{0,16})"`}),
		},
	}

	numbers := Zero(Mode{
		ClassName: "number",
		Variants: []*Mode{
			{ClassName: "number", Begin: `\b(0b[01']+)`},
			{ClassName: "number", Begin: `(-?)\b([\d']+(\.[\d']*)?|\.[\d']+)((ll|LL|l|L)(u|U)?|(u|U)(ll|LL|l|L)?|f|F|b|B)`},
			{ClassName: "number", Begin: `(-?)(\b0[xX][a-fA-F0-9']+|(\b[\d']+(\.[\d']*)?|\.[\d']+)([eE][-+]?[\d']+)?)`},
		},
	})

	cppKeywords := Keywords{
		"keyword": "int float while private char char8_t char16_t char32_t catch import module export virtual operator sizeof " +
			"dynamic_cast|10 typedef const_cast|10 const for static_cast|10 union namespace " +
			"unsigned long volatile static protected bool template mutable if public friend " +
			"do goto auto void enum else break extern using asm case typeid wchar_t " +
			"short reinterpret_cast|10 default double register explicit signed typename try this " +
			"switch continue inline delete alignas alignof constexpr consteval constinit decltype " +
			"concept co_await co_return co_yield requires " +
			"noexcept static_assert thread_local restrict final override " +
			"atomic_bool atomic_char atomic_schar " +
			"atomic_uchar atomic_short atomic_ushort atomic_int atomic_uint atomic_long atomic_ulong atomic_llong " +
			"atomic_ullong new throw return " +
			"and and_eq bitand bitor compl not not_eq or or_eq xor xor_eq",
		"built_in": "std string wstring cin cout cerr clog stdin stdout stderr stringstream istringstream ostringstream " +
			"auto_ptr deque list queue stack vector map set pair bitset multiset multimap unordered_set " +
			"unordered_map unordered_multiset unordered_multimap priority_queue make_pair array shared_ptr abort terminate abs acos " +
			"asin atan2 atan calloc ceil cosh cos exit exp fabs floor fmod fprintf fputs free frexp " +
			"fscanf future isalnum isalpha iscntrl isdigit isgraph islower isprint ispunct isspace isupper " +
			"isxdigit tolower toupper labs ldexp log10 log malloc realloc memchr memcmp memcpy memset modf pow " +
			"printf putchar puts scanf sinh sin snprintf sprintf sqrt sscanf strcat strchr strcmp " +
			"strcpy strcspn strlen strncat strncmp strncpy strpbrk strrchr strspn strstr tanh tan " +
			"vfprintf vprintf vsprintf endl initializer_list unique_ptr _Bool complex _Complex imaginary _Imaginary",
		"literal": "true false nullptr NULL",
	}

	preprocessor := &Mode{
		ClassName: "meta",
		Begin:     `#\s*[a-z]+\b`,
		End:       `$`,
		Keywords: Keywords{
			"meta-keyword": "if else elif endif define undef warning error line pragma _Pragma ifdef ifndef include",
		},
		Contains: []*Mode{
			Zero(Mode{Begin: `\\\n`}),
			Variant(strings, func(m *Mode) { m.ClassName = "meta-string" }),
			{ClassName: "meta-string", Begin: `<.*?>`},
			lineComment,
			CBlockCommentMode,
		},
	}

	titleMode := Zero(Mode{ClassName: "title", Begin: optionalRe(namespaceRe) + IdentRe})
	functionTitle := optionalRe(namespaceRe) + IdentRe + `\s*\(`

	expressionContains := []*Mode{
		preprocessor,
		cppPrimitiveTypes,
		lineComment,
		CBlockCommentMode,
		numbers,
		strings,
	}

	parenExpr := &Mode{
		Begin:        `\(`,
		End:          `\)`,
		Keywords:     cppKeywords,
		Relevance:    0,
		HasRelevance: true,
	}
	parenExpr.Contains = append(append([]*Mode{}, expressionContains...), Self)

	expressionBase := &Mode{
		Keywords:     cppKeywords,
		Contains:     append(append([]*Mode{}, expressionContains...), parenExpr),
		HasRelevance: true,
		Relevance:    0,
	}
	expressionContext := &Mode{
		Variants: []*Mode{
			Variant(expressionBase, func(m *Mode) { m.Begin = `=`; m.End = `;` }),
			Variant(expressionBase, func(m *Mode) { m.Begin = `\(`; m.End = `\)` }),
			Variant(expressionBase, func(m *Mode) { m.BeginKeywords = "new throw return else"; m.End = `;` }),
		},
	}

	functionDeclaration := &Mode{
		ClassName:   "function",
		Begin:       `(` + functionTypeRe + `[\*&\s]+)+` + functionTitle,
		ReturnBegin: true,
		End:         `[{;=]`,
		ExcludeEnd:  true,
		Keywords:    cppKeywords,
		Illegal:     `[^\w\s\*&:<>.]`,
		Contains: []*Mode{
			Zero(Mode{Begin: decltypeAutoRe, Keywords: cppKeywords}),
			Zero(Mode{Begin: functionTitle, ReturnBegin: true, Contains: []*Mode{titleMode}}),
			{
				ClassName:    "params",
				Begin:        `\(`,
				End:          `\)`,
				Keywords:     cppKeywords,
				HasRelevance: true,
				Relevance:    0,
				Contains: []*Mode{
					lineComment,
					CBlockCommentMode,
					strings,
					numbers,
					cppPrimitiveTypes,
					{
						Begin:        `\(`,
						End:          `\)`,
						Keywords:     cppKeywords,
						HasRelevance: true,
						Relevance:    0,
						Contains: []*Mode{
							Self,
							lineComment,
							CBlockCommentMode,
							strings,
							numbers,
							cppPrimitiveTypes,
						},
					},
				},
			},
			cppPrimitiveTypes,
			lineComment,
			CBlockCommentMode,
			preprocessor,
		},
	}

	classMode := &Mode{
		ClassName:     "class",
		BeginKeywords: "enum class struct union",
		End:           `[{;:<>=]`,
		Contains: []*Mode{
			{BeginKeywords: "final class struct"},
			TitleMode,
		},
	}
	container := &Mode{
		Begin:    `\b(deque|list|queue|priority_queue|pair|stack|vector|map|set|bitset|multiset|multimap|unordered_map|unordered_set|unordered_multiset|unordered_multimap|array)\s*<`,
		End:      `>`,
		Keywords: cppKeywords,
		Contains: []*Mode{Self, cppPrimitiveTypes},
	}

	contains := []*Mode{expressionContext, functionDeclaration}
	contains = append(contains, expressionContains...)
	contains = append(contains,
		preprocessor,
		container,
		&Mode{Begin: IdentRe + `::`, Keywords: cppKeywords},
		classMode,
	)

	return &Language{
		Name:              "C",
		Aliases:           []string{"h"},
		DisableAutodetect: true,
		Keywords:          cppKeywords,
		Illegal:           `</`,
		Contains:          contains,
	}
}
