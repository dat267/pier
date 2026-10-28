package highlight

import ()

func init() { RegisterLanguage("lua", Lua()) }

var luaLongBracketStart = mustCompileJS(`\A\[=*\[`)

// Lua returns the Lua language definition (a port of languages/lua.js).
func Lua() *Language {
	const openingLongBracket = `\[=*\[`
	const closingLongBracket = `\]=*\]`

	longBrackets := &Mode{
		Begin:    openingLongBracket,
		End:      closingLongBracket,
		Contains: []*Mode{Self},
	}

	// The reference's `--(?!\[=*\[)` negative lookahead: a line comment must not
	// be followed by a long bracket opener.
	lineComment := commentMode(`--`, `$`)
	lineComment.beforeBegin = func(m *match, resp *Response) {
		if luaLongBracketStart.MatchString(m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}
	blockComment := commentMode(`--`+openingLongBracket, closingLongBracket)
	blockComment.HasRelevance = true
	blockComment.Relevance = 10
	blockComment.Contains = append(append([]*Mode{}, blockComment.Contains...), longBrackets)

	keywords := Keywords{
		"$pattern": UnderscoreIdentRe,
		"literal":  "true false nil",
		"keyword":  "and break do else elseif end for goto if in local not or repeat return then until while",
		"built_in": "_G _ENV _VERSION __index __newindex __mode __call __metatable __tostring __len " +
			"__gc __add __sub __mul __div __mod __pow __concat __unm __eq __lt __le assert " +
			"collectgarbage dofile error getfenv getmetatable ipairs load loadfile loadstring " +
			"module next pairs pcall print rawequal rawget rawset require select setfenv " +
			"setmetatable tonumber tostring type unpack xpcall arg self " +
			"coroutine resume yield status wrap create running debug getupvalue " +
			"debug sethook getmetatable gethook setmetatable setlocal traceback setfenv getinfo setupvalue getlocal getregistry getfenv " +
			"io lines write close flush open output type read stderr stdin input stdout popen tmpfile " +
			"math log max acos huge ldexp pi cos tanh pow deg tan cosh sinh random randomseed frexp ceil floor rad abs sqrt modf asin min mod fmod log10 atan2 exp sin atan " +
			"os exit setlocale date getenv difftime remove time clock tmpname rename execute package preload loadlib loaded loaders cpath config path seeall " +
			"string sub upper len gfind rep find match char dump gmatch reverse byte format gsub lower " +
			"table setn insert getn foreachi maxn foreach concat sort remove",
	}

	titleVariant := Variant(TitleMode, func(m *Mode) {
		m.Begin = `([_a-zA-Z]\w*\.)*([_a-zA-Z]\w*:)?[_a-zA-Z]\w*`
	})
	params := &Mode{
		ClassName:      "params",
		Begin:          `\(`,
		EndsWithParent: true,
		Contains:       []*Mode{lineComment, blockComment},
	}
	function := &Mode{
		ClassName:     "function",
		BeginKeywords: "function",
		End:           `\)`,
		Contains:      []*Mode{titleVariant, params},
	}
	longString := &Mode{
		ClassName:    "string",
		Begin:        openingLongBracket,
		End:          closingLongBracket,
		Contains:     []*Mode{longBrackets},
		HasRelevance: true,
		Relevance:    5,
	}
	contains := []*Mode{
		lineComment,
		blockComment,
		function,
		CNumberMode,
		AposStringMode,
		QuoteStringMode,
		longString,
	}
	function.Contains = append(function.Contains, lineComment, blockComment)

	return &Language{
		Name:     "Lua",
		Keywords: keywords,
		Contains: contains,
	}
}
