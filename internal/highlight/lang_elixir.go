package highlight

import (
	"strings"
)

func init() { RegisterLanguage("elixir", Elixir()) }

var (
	elixirSigilDelimiters = mustCompileJS(`\A[/|([{<"']`)
	elixirSymbolGuard     = mustCompileJS(`\A[\s:]`)
	elixirRegexpGuard     = mustCompileJS(`\A\d+\s*[,\]]`)
)

// Elixir returns the Elixir language definition (a port of languages/elixir.js).
func Elixir() *Language {
	const identRe = `[a-zA-Z_][a-zA-Z0-9_.]*(!|\?)?`
	const methodRe = "[a-zA-Z_]\\w*[!?=]?|[-+~]@|<<|>>|=~|===?|<=>|[<>]=?|\\*\\*|[-/+%^&*~`|]|\\[\\]=?"

	keywords := Keywords{
		"$pattern": "[a-zA-Z_][a-zA-Z0-9_.]*(!|\\?)?",
		"keyword": "and false then defined module in return redo retry end for true self when next until do begin unless " +
			"nil break not case cond alias while ensure or include use alias fn quote require import with|0",
	}

	subst := &Mode{ClassName: "subst", Begin: `#\{`, End: `\}`, Keywords: keywords}

	number := &Mode{
		ClassName:    "number",
		Begin:        `(\b0o[0-7_]+)|(\b0b[01_]+)|(\b0x[0-9a-fA-F_]+)|(-?\b[1-9][0-9_]*(\.[0-9_]+([eE][-+]?[0-9]+)?)?)`,
		HasRelevance: true,
		Relevance:    0,
	}

	// Sigil inner body: a mode whose only job is to close the parent once the
	// delimited body ends.
	sigilBodyVariants := []*Mode{
		{Begin: `"`, End: `"`},
		{Begin: `'`, End: `'`},
		{Begin: `/`, End: `/`},
		{Begin: `\|`, End: `\|`},
		{Begin: `\(`, End: `\)`},
		{Begin: `\[`, End: `\]`},
		{Begin: `\{`, End: `\}`},
		{Begin: `<`, End: `>`},
	}
	sigilBody := &Mode{
		EndsParent: true,
		Contains: []*Mode{
			{Contains: []*Mode{BackslashEscape, subst}, Variants: sigilBodyVariants},
		},
	}

	lowerSigil := &Mode{ClassName: "string", Begin: `~[a-z]`, Contains: []*Mode{sigilBody}}
	lowerSigil.beforeBegin = func(m *match, resp *Response) {
		if !startsWith(elixirSigilDelimiters, m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}
	upperSigil := &Mode{ClassName: "string", Begin: `~[A-Z]`, Contains: sigilBodyVariants}
	upperSigil.beforeBegin = func(m *match, resp *Response) {
		if !startsWith(elixirSigilDelimiters, m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}

	stringBase := &Mode{ClassName: "string", Contains: []*Mode{BackslashEscape, subst}}
	stringBase.Variants = []*Mode{
		Variant(stringBase, func(m *Mode) { m.Begin = `"""`; m.End = `"""` }),
		Variant(stringBase, func(m *Mode) { m.Begin = `'''`; m.End = `'''` }),
		Variant(stringBase, func(m *Mode) { m.Begin = `~S"""`; m.End = `"""`; m.Contains = nil }),
		Variant(stringBase, func(m *Mode) { m.Begin = `~S"`; m.End = `"`; m.Contains = nil }),
		Variant(stringBase, func(m *Mode) { m.Begin = `~S'''`; m.End = `'''`; m.Contains = nil }),
		Variant(stringBase, func(m *Mode) { m.Begin = `~S'`; m.End = `'`; m.Contains = nil }),
		Variant(stringBase, func(m *Mode) { m.Begin = `'`; m.End = `'` }),
		Variant(stringBase, func(m *Mode) { m.Begin = `"`; m.End = `"` }),
	}

	function := &Mode{
		ClassName:     "function",
		BeginKeywords: "def defp defmacro",
		End:           `\B\b`,
		Contains: []*Mode{
			Variant(TitleMode, func(m *Mode) { m.Begin = identRe; m.EndsParent = true }),
		},
	}
	classMode := Variant(function, func(m *Mode) {
		m.ClassName = "class"
		m.BeginKeywords = "defimpl defmodule defprotocol defrecord"
		m.End = `\bdo\b|$|;`
	})

	keywordSymbol := &Mode{ClassName: "symbol", Begin: `:`, HasRelevance: true, Relevance: 0}
	keywordSymbol.beforeBegin = func(m *match, resp *Response) {
		if startsWith(elixirSymbolGuard, m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}
	keywordSymbol.Contains = []*Mode{stringBase, {Begin: methodRe}}

	identSymbol := &Mode{ClassName: "symbol", Begin: identRe + `:`, HasRelevance: true, Relevance: 0}
	identSymbol.beforeBegin = func(m *match, resp *Response) {
		if strings.HasPrefix(m.input[m.index+len(m.lexeme):], ":") {
			resp.IgnoreMatch()
		}
	}

	regexpGuardMode := &Mode{Begin: `/: `, HasRelevance: true, Relevance: 0, Contains: []*Mode{number}}
	regexpGuardMode.beforeBegin = func(m *match, resp *Response) {
		if !startsWith(elixirRegexpGuard, m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}

	regexpBase := &Mode{ClassName: "regexp", Illegal: `\n`, Contains: []*Mode{BackslashEscape, subst}}
	regexpBase.Variants = []*Mode{
		Variant(regexpBase, func(m *Mode) { m.Begin = `/`; m.End = `/[a-z]*` }),
		Variant(regexpBase, func(m *Mode) { m.Begin = `%r\[`; m.End = `\][a-z]*` }),
	}

	const reStarters = `!|!=|!==|%|%=|&|&&|&=|\*|\*=|\+|\+=|,|-|-=|/=|/|:|;|<<|<<=|<=|<|===|==|=|>>>=|>>=|>=|>>>|>>|>|\?|\[|\{|\(|\^|\^=|\||\|=|\|\||~`
	regexpContainer := &Mode{
		Begin:        `(` + reStarters + `)\s*`,
		HasRelevance: true,
		Relevance:    0,
		Contains: []*Mode{
			HashCommentMode,
			regexpGuardMode,
			regexpBase,
		},
	}

	variable := &Mode{ClassName: "variable", Begin: `(\$\W)|((\$|@@?)(\w+))`}

	defaults := []*Mode{
		stringBase,
		upperSigil,
		lowerSigil,
		HashCommentMode,
		classMode,
		function,
		{Begin: `::`},
		keywordSymbol,
		identSymbol,
		number,
		variable,
		{Begin: `->`},
		regexpContainer,
	}
	subst.Contains = defaults

	return &Language{
		Name:     "Elixir",
		Keywords: keywords, Contains: defaults,
	}
}
