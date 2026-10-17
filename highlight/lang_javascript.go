package highlight

import (
	"regexp"
	"strings"
)

func init() {
	RegisterLanguage("javascript", JavaScript())
	RegisterLanguage("typescript", TypeScript())
}

var (
	jsObjectAttrGuard = regexp.MustCompile(`(?m)\A(?:((//.*$)|(/\*(\*[^/]|[^*])*\*/))\s*)*` + jsIdentRe + `\s*:`)
	jsAttrNameGuard   = regexp.MustCompile(`\A\s*:`)
	jsGetSetGuard     = regexp.MustCompile(`\A` + jsIdentRe + `\(`)
)

const jsIdentRe = `[A-Za-z$_][0-9A-Za-z$_]*`

// JSParamsContains returns the rule set the reference exports for TypeScript.
func jsParamsContains() []*Mode { return append([]*Mode{}, jsComment().Contains...) }

func jsComment() *Mode {
	return &Mode{ClassName: "comment", Variants: []*Mode{
		jsJSDoc(),
		Variant(CBlockCommentMode, func(*Mode) {}),
		Variant(CLineCommentMode, func(*Mode) {}),
	}}
}

func jsJSDoc() *Mode {
	// The reference's JSDOC_COMMENT begin `/\*\*(?!\/)` lookahead and the
	// doctag's `(?=\s*(-)|$)` and `(?=[^\n])\s` lookarounds.
	mode := &Mode{
		ClassName:    "comment",
		Begin:        `/\*\*`,
		End:          `\*/`,
		HasRelevance: true,
		Relevance:    0,
	}
	mode.beforeBegin = func(m *match, resp *Response) {
		if next := m.index + len(m.lexeme); next < len(m.input) && m.input[next] == '/' {
			resp.IgnoreMatch()
		}
	}
	variable := &Mode{ClassName: "variable", Begin: jsIdentRe, EndsParent: true}
	variable.beforeBegin = func(m *match, resp *Response) {
		rest := m.input[m.index+len(m.lexeme):]
		i := 0
		for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t' || rest[i] == '\r' || rest[i] == '\f' || rest[i] == '\v') {
			i++
		}
		if i < len(rest) && rest[i] != '-' && rest[i] != '\n' {
			resp.IgnoreMatch()
		}
	}
	doctag := &Mode{
		ClassName: "doctag",
		Begin:     `@[A-Za-z]+`,
		Contains: []*Mode{
			Zero(Mode{ClassName: "type", Begin: `\{`, End: `\}`}),
			variable,
			Zero(Mode{Begin: `[ \t\r\f\v]`}),
		},
	}
	mode.Contains = []*Mode{doctag}
	return mode
}

// JavaScript returns the JavaScript language definition (a port of
// languages/javascript.js).
func JavaScript() *Language { return buildJS(false) }

// TypeScript returns the TypeScript definition, which is JavaScript plus the
// TypeScript keyword sets, decorators, namespace/interface modes and a
// simplified shebang/use-strict rule.
func TypeScript() *Language { return buildJS(true) }

func buildJS(typescript bool) *Language {
	keywords := "as in of if for while finally var new function do return void else break catch " +
		"instanceof with throw case default try switch continue typeof delete let yield const class " +
		"debugger async await static import from export extends"
	literals := "true false null undefined NaN Infinity"
	types := "Intl DataView Number Math Date String RegExp Object Function Boolean Error Symbol Set Map WeakSet WeakMap Proxy Reflect JSON Promise Float64Array Int16Array Int32Array Int8Array Uint16Array Uint32Array Float32Array Array Uint8Array Uint8ClampedArray ArrayBuffer BigInt64Array BigUint64Array BigInt"
	errorTypes := "EvalError InternalError RangeError ReferenceError SyntaxError TypeError URIError"
	builtInGlobals := "setInterval setTimeout clearInterval clearTimeout require exports eval isFinite isNaN parseFloat parseInt decodeURI decodeURIComponent encodeURI encodeURIComponent escape unescape"
	builtInVariables := "arguments this super console window document localStorage module global"
	builtIns := builtInGlobals + " " + builtInVariables + " " + types + " " + errorTypes

	if typescript {
		keywords += " type namespace typedef interface public private protected implements declare abstract readonly"
		builtIns += " any void number boolean string object never enum"
	}
	kw := Keywords{"$pattern": jsIdentRe, "keyword": keywords, "literal": literals, "built_in": builtIns}

	fragment := &Mode{Begin: `<>`, End: `</>`}
	xmlTag := &Mode{Begin: `<[A-Za-z0-9\\._:-]+`, End: `\/[A-Za-z0-9\\._:-]+>|\/>`}
	xmlTag.OnBegin = func(m *match, resp *Response) {
		after := m.index + len(m.lexeme)
		if after < len(m.input) {
			switch m.input[after] {
			case '<':
				resp.IgnoreMatch()
				return
			case '>':
				tag := "</" + m.lexeme[1:]
				if !strings.Contains(m.input[after:], tag) {
					resp.IgnoreMatch()
					return
				}
			}
		}
	}

	const decimalDigits = `[0-9](_?[0-9])*`
	frac := `\.(` + decimalDigits + `)`
	const decimalInteger = `0|[1-9](_?[0-9])*|0[0-7]*[89][0-9]*`
	number := Zero(Mode{
		ClassName: "number",
		Variants: []*Mode{
			{ClassName: "number", Begin: `(\b(` + decimalInteger + `)((` + frac + `)|\.)?|(` + frac + `))[eE][+-]?(` + decimalDigits + `)\b`},
			{ClassName: "number", Begin: `\b(` + decimalInteger + `)\b((` + frac + `)\b|\.)?|(` + frac + `)\b`},
			{ClassName: "number", Begin: `\b(0|[1-9](_?[0-9])*)n\b`},
			{ClassName: "number", Begin: `\b0[xX][0-9a-fA-F](_?[0-9a-fA-F])*n?\b`},
			{ClassName: "number", Begin: `\b0[bB][0-1](_?[0-1])*n?\b`},
			{ClassName: "number", Begin: `\b0[oO][0-7](_?[0-7])*n?\b`},
			{ClassName: "number", Begin: `\b0[0-7]+n?\b`},
		},
	})

	subst := &Mode{ClassName: "subst", Begin: `\$\{`, End: `\}`, Keywords: kw}

	htmlTemplate := &Mode{
		Begin: `html` + "`",
		Starts: &Mode{
			End:         "`",
			Contains:    []*Mode{BackslashEscape, subst},
			SubLanguage: []string{"xml"},
		},
	}
	cssTemplate := &Mode{
		Begin: `css` + "`",
		Starts: &Mode{
			End:         "`",
			Contains:    []*Mode{BackslashEscape, subst},
			SubLanguage: []string{"css"},
		},
	}
	templateString := &Mode{
		ClassName: "string",
		Begin:     "`",
		End:       "`",
		Contains:  []*Mode{BackslashEscape, subst},
	}

	decorator := Zero(Mode{ClassName: "meta", Begin: `@` + jsIdentRe})
	comment := jsComment()

	substInternals := []*Mode{
		AposStringMode,
		QuoteStringMode,
		htmlTemplate,
		cssTemplate,
		templateString,
		number,
		RegexpMode,
	}
	substSelf := &Mode{Begin: `\{`, End: `\}`, Keywords: kw}
	substSelf.Contains = append([]*Mode{Self}, substInternals...)
	substInternals = append(substInternals, substSelf)
	subst.Contains = substInternals

	paramsContains := append([]*Mode{comment}, subst.Contains...)
	paramsParen := &Mode{Begin: `\(`, End: `\)`, Keywords: kw}
	paramsParen.Contains = append([]*Mode{Self}, paramsContains...)
	paramsContains = append(paramsContains, paramsParen)
	if typescript {
		// The reference pushes DECORATOR onto the exported PARAMS_CONTAINS.
		paramsContains = append(paramsContains, decorator)
	}

	params := &Mode{
		ClassName:    "params",
		Begin:        `\(`,
		End:          `\)`,
		ExcludeBegin: true,
		ExcludeEnd:   true,
		Keywords:     kw,
		Contains:     paramsContains,
	}

	attrName := Zero(Mode{ClassName: "attr", Begin: jsIdentRe})
	attrName.beforeBegin = func(m *match, resp *Response) {
		if !jsAttrNameGuard.MatchString(m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}
	objectAttr := Zero(Mode{
		Begin:    `[{,\n]\s*`,
		Contains: []*Mode{attrName},
	})
	objectAttr.beforeBegin = func(m *match, resp *Response) {
		if !jsObjectAttrGuard.MatchString(m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}

	arrowParams := &Mode{
		ClassName: "params",
		Variants: []*Mode{
			Zero(Mode{ClassName: "params", Begin: UnderscoreIdentRe}),
			{ClassName: "params", Begin: `\(\s*\)`, Skip: true},
			{
				ClassName:    "params",
				Begin:        `\(`,
				End:          `\)`,
				ExcludeBegin: true,
				ExcludeEnd:   true,
				Keywords:     kw,
				Contains:     paramsContains,
			},
		},
	}
	arrowFunction := &Mode{
		ClassName:   "function",
		Begin:       `(\([^()]*(\([^()]*(\([^()]*\)[^()]*)*\)[^()]*)*\)|` + UnderscoreIdentRe + `)\s*=>`,
		ReturnBegin: true,
		End:         `\s*=>`,
		Contains:    []*Mode{arrowParams},
	}

	// The reference puts subLanguage/contains on the outer JSX mode and only
	// begin/end on each variant; Variant clones the base so the variants
	// inherit them (a partial Variants slice would drop the xml sub-language).
	jsxBase := &Mode{
		SubLanguage: []string{"xml"},
		Contains: []*Mode{
			{Begin: xmlTag.Begin, End: xmlTag.End, Skip: true, Contains: []*Mode{Self}},
		},
	}
	jsxBase.Variants = []*Mode{
		Variant(jsxBase, func(m *Mode) { m.Begin = fragment.Begin; m.End = fragment.End }),
		Variant(jsxBase, func(m *Mode) {
			m.Begin = xmlTag.Begin
			m.OnBegin = xmlTag.OnBegin
			m.End = xmlTag.End
		}),
	}
	jsx := jsxBase

	valueContainer := Zero(Mode{
		Begin:    `(` + reStartersRe + `|\b(case|return|throw)\b)\s*`,
		Keywords: Keywords{"keyword": "return throw case"},
		Contains: []*Mode{
			comment,
			RegexpMode,
			arrowFunction,
			Zero(Mode{Begin: `,`}),
			{Begin: `\s`, End: `\s*`, Skip: true},
			jsx,
		},
	})

	functionMode := &Mode{
		ClassName:     "function",
		BeginKeywords: "function",
		End:           `[{;]`,
		ExcludeEnd:    true,
		Keywords:      kw,
		Illegal:       `%`,
		Contains: []*Mode{
			Self,
			Variant(TitleMode, func(m *Mode) { m.Begin = jsIdentRe }),
			params,
		},
	}
	if typescript {
		functionMode.Relevance = 0
		functionMode.HasRelevance = true
	}
	functionExpr := &Mode{
		ClassName:   "function",
		Begin:       UnderscoreIdentRe + `\([^()]*(\([^()]*(\([^()]*\)[^()]*)*\)[^()]*)*\)\s*\{`,
		ReturnBegin: true,
		Contains: []*Mode{
			params,
			Variant(TitleMode, func(m *Mode) { m.Begin = jsIdentRe }),
		},
	}
	classMode := &Mode{
		ClassName:     "class",
		BeginKeywords: "class",
		End:           `[{;=]`,
		ExcludeEnd:    true,
		Illegal:       `[:"[\]]`,
		Contains: []*Mode{
			{BeginKeywords: "extends"},
			UnderscoreTitleMode,
		},
	}
	constructorMode := &Mode{Begin: `\bconstructor`, ReturnBegin: true, End: `[{;]`, ExcludeEnd: true}
	constructorMode.Contains = []*Mode{
		Variant(TitleMode, func(m *Mode) { m.Begin = jsIdentRe }),
		Self,
		params,
	}
	getSet := &Mode{
		Begin:    `(get|set)\s+`,
		End:      `\{`,
		Keywords: Keywords{"keyword": "get set"},
		Contains: []*Mode{
			Variant(TitleMode, func(m *Mode) { m.Begin = jsIdentRe }),
			{Begin: `\(\)`},
			params,
		},
	}
	getSet.beforeBegin = func(m *match, resp *Response) {
		if !jsGetSetGuard.MatchString(m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}

	useStrictBegin := `^\s*['"]use (strict|asm)['"]`
	if typescript {
		useStrictBegin = `^\s*['"]use strict['"]`
	}
	useStrict := &Mode{
		ClassName:    "meta",
		HasRelevance: true,
		Relevance:    10,
		Begin:        useStrictBegin,
	}
	keywordGuard := &Mode{
		Variants: []*Mode{
			{Begin: `\.` + jsIdentRe},
			{Begin: `\$` + jsIdentRe},
		},
		HasRelevance: true,
		Relevance:    0,
	}

	shebang := Shebang("node", 5)
	if typescript {
		// TypeScript gets the plain shebang rule.
		shebang = Shebang("", 0)
	}
	contains := []*Mode{
		shebang,
		useStrict,
		AposStringMode,
		QuoteStringMode,
		htmlTemplate,
		cssTemplate,
		templateString,
		comment,
		number,
		objectAttr,
		valueContainer,
		functionMode,
		{BeginKeywords: "while if switch catch for"},
		functionExpr,
		keywordGuard,
		classMode,
		constructorMode,
		getSet,
		{Begin: `\$[(.]`},
	}
	if typescript {
		contains = append(contains,
			decorator,
			&Mode{BeginKeywords: "namespace", End: `\{`, ExcludeEnd: true},
			&Mode{BeginKeywords: "interface", End: `\{`, ExcludeEnd: true, Keywords: Keywords{"keyword": "interface extends"}},
		)
	}
	name := "Javascript"
	aliases := []string{"js", "jsx", "mjs", "cjs"}
	if typescript {
		name = "TypeScript"
		aliases = []string{"ts", "tsx"}
	}
	return &Language{
		Name:    name,
		Aliases: aliases,
		Mode: Mode{
			Keywords: kw,
			Illegal:  `#[^$_A-Za-z]`,
			Contains: contains,
		},
	}
}

// reStartersRe mirrors core.js RE_STARTERS_RE. The reference stores it as a JS
// string literal, where `\\+` is the two characters `\+`; a Go raw string needs
// the regex source directly (`\+`), not the JS escaping.
const reStartersRe = `!|!=|!==|%|%=|&|&&|&=|\*|\*=|\+|\+=|,|-|-=|/=|/|:|;|<<|<<=|<=|<|===|==|=|>>>=|>>=|>=|>>>|>>|>|\?|\[|\{|\(|\^|\^=|\||\|=|\|\||~`
