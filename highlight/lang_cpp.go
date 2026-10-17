package highlight

import gostrings "strings"

func init() { RegisterLanguage("cpp", CPP()) }

// CPP returns the C++ language definition (a port of languages/cpp.js). The
// shared C modes are rebuilt here rather than reused from C() so each language
// keeps its own compiled tree and keyword set.
func CPP() *Language {
	lineComment := commentMode(`//`, `$`)
	lineComment.Contains = append(lineComment.Contains, &Mode{Begin: `\\\n`})

	const decltypeAutoRe = `decltype\(auto\)`
	const namespaceRe = `[a-zA-Z_]\w*::`
	const templateArgumentRe = `<[^<>]+>`
	functionTypeRe := "(" + decltypeAutoRe + "|" + optionalRe(namespaceRe) + `[a-zA-Z_]\w*` + optionalRe(templateArgumentRe) + ")"
	functionTitle := optionalRe(namespaceRe) + IdentRe + `\s*\(`

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
		"built_in":         "_Bool _Complex _Imaginary",
		"_relevance_hints": gostrings.Join(cppCommonHints, " "),
		"literal":          "true false nullptr NULL",
	}

	preprocessor := &Mode{
		ClassName: "meta",
		Begin:     `#\s*[a-z]+\b`,
		End:       `$`,
		Keywords:  Keywords{"meta-keyword": "if else elif endif define undef warning error line pragma _Pragma ifdef ifndef include"},
		Contains: []*Mode{
			Zero(Mode{Begin: `\\\n`}),
			Variant(strings, func(m *Mode) { m.ClassName = "meta-string" }),
			{ClassName: "meta-string", Begin: `<.*?>`},
			lineComment,
			CBlockCommentMode,
		},
	}
	titleMode := Zero(Mode{ClassName: "title", Begin: optionalRe(namespaceRe) + IdentRe})

	// The reference's FUNCTION_DISPATCH has `\b(?!decltype)(?!if)(?!for)(?!while)`
	// lookaheads and a `(?=\s*\()` lookahead; both become a beforeBegin guard.
	functionDispatch := Zero(Mode{
		ClassName: "function.dispatch",
		Keywords:  cppKeywords,
		Begin:     `\b` + IdentRe,
	})
	functionDispatch.beforeBegin = func(m *match, resp *Response) {
		switch m.lexeme {
		case "decltype", "if", "for", "while":
			resp.IgnoreMatch()
			return
		}
		rest := m.input[m.index+len(m.lexeme):]
		i := 0
		for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t' || rest[i] == '\n' || rest[i] == '\r') {
			i++
		}
		if i >= len(rest) || rest[i] != '(' {
			resp.IgnoreMatch()
		}
	}

	expressionContains := []*Mode{
		functionDispatch,
		preprocessor,
		cppPrimitiveTypes,
		lineComment,
		CBlockCommentMode,
		numbers,
		strings,
	}

	parenExpr := Zero(Mode{
		Begin:    `\(`,
		End:      `\)`,
		Keywords: cppKeywords,
	})
	parenExpr.Contains = append(append([]*Mode{}, expressionContains...), Self)

	expressionBase := Zero(Mode{
		Keywords: cppKeywords,
		Contains: append(append([]*Mode{}, expressionContains...), parenExpr),
	})
	expressionContext := &Mode{
		Variants: []*Mode{
			Variant(expressionBase, func(m *Mode) { m.Begin = `=`; m.End = `;` }),
			Variant(expressionBase, func(m *Mode) { m.Begin = `\(`; m.End = `\)` }),
			Variant(expressionBase, func(m *Mode) { m.BeginKeywords = "new throw return else"; m.End = `;` }),
		},
	}

	innerParen := Zero(Mode{
		Begin:    `\(`,
		End:      `\)`,
		Keywords: cppKeywords,
		Contains: []*Mode{
			Self,
			lineComment,
			CBlockCommentMode,
			strings,
			numbers,
			cppPrimitiveTypes,
		},
	})
	params := Zero(Mode{
		ClassName: "params",
		Begin:     `\(`,
		End:       `\)`,
		Keywords:  cppKeywords,
		Contains: []*Mode{
			lineComment,
			CBlockCommentMode,
			strings,
			numbers,
			cppPrimitiveTypes,
			innerParen,
		},
	})
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
			Zero(Mode{Begin: `::`}),
			{Begin: `:`, EndsWithParent: true, Contains: []*Mode{strings, numbers}},
			params,
			cppPrimitiveTypes,
			lineComment,
			CBlockCommentMode,
			preprocessor,
		},
	}

	container := &Mode{
		Begin:    `\b(deque|list|queue|priority_queue|pair|stack|vector|map|set|bitset|multiset|multimap|unordered_map|unordered_set|unordered_multiset|unordered_multimap|array)\s*<`,
		End:      `>`,
		Keywords: cppKeywords,
		Contains: []*Mode{Self, cppPrimitiveTypes},
	}

	contains := []*Mode{expressionContext, functionDeclaration, functionDispatch}
	contains = append(contains, expressionContains...)
	contains = append(contains,
		preprocessor,
		container,
		&Mode{Begin: IdentRe + `::`, Keywords: cppKeywords},
		&Mode{
			ClassName:     "class",
			BeginKeywords: "enum class struct union",
			End:           `[{;:<>=]`,
			Contains: []*Mode{
				{BeginKeywords: "final class struct"},
				TitleMode,
			},
		},
	)

	return &Language{
		Name:             "C++",
		Aliases:          []string{"cc", "c++", "h++", "hpp", "hh", "hxx", "cxx"},
		ClassNameAliases: map[string]string{"function.dispatch": "built_in"},
		Mode: Mode{
			Keywords: cppKeywords,
			Illegal:  `</`,
			Contains: contains,
		},
	}
}

var cppCommonHints = []string{
	"asin", "atan2", "atan", "calloc", "ceil", "cosh", "cos", "exit", "exp", "fabs",
	"floor", "fmod", "fprintf", "fputs", "free", "frexp", "auto_ptr", "deque", "list",
	"queue", "stack", "vector", "map", "set", "pair", "bitset", "multiset", "multimap",
	"unordered_set", "fscanf", "future", "isalnum", "isalpha", "iscntrl", "isdigit",
	"isgraph", "islower", "isprint", "ispunct", "isspace", "isupper", "isxdigit",
	"tolower", "toupper", "labs", "ldexp", "log10", "log", "malloc", "realloc",
	"memchr", "memcmp", "memcpy", "memset", "modf", "pow", "printf", "putchar", "puts",
	"scanf", "sinh", "sin", "snprintf", "sprintf", "sqrt", "sscanf", "strcat",
	"strchr", "strcmp", "strcpy", "strcspn", "strlen", "strncat", "strncmp", "strncpy",
	"strpbrk", "strrchr", "strspn", "strstr", "tanh", "tan", "unordered_map",
	"unordered_multiset", "unordered_multimap", "priority_queue", "make_pair", "array",
	"shared_ptr", "abort", "terminate", "abs", "acos", "vfprintf", "vprintf",
	"vsprintf", "endl", "initializer_list", "unique_ptr", "complex", "imaginary", "std",
	"string", "wstring", "cin", "cout", "cerr", "clog", "stdin", "stdout", "stderr",
	"stringstream", "istringstream", "ostringstream",
}
