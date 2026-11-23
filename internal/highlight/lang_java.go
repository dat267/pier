package highlight

import "unicode/utf8"

func init() { RegisterLanguage("java", Java()) }

func javaNumeric() *Mode {
	const decimalDigits = `[0-9](_*[0-9])*`
	frac := `\.(` + decimalDigits + `)`
	const hexDigits = `[0-9a-fA-F](_*[0-9a-fA-F])*`
	return Zero(Mode{
		ClassName: "number",
		Variants: []*Mode{
			{ClassName: "number", Begin: `(\b(` + decimalDigits + `)((` + frac + `)|\.)?|(` + frac + `))[eE][+-]?(` + decimalDigits + `)[fFdD]?\b`},
			{ClassName: "number", Begin: `\b(` + decimalDigits + `)((` + frac + `)[fFdD]?\b|\.([fFdD]\b)?)`},
			{ClassName: "number", Begin: `(` + frac + `)[fFdD]?\b`},
			{ClassName: "number", Begin: `\b(` + decimalDigits + `)[fFdD]\b`},
			{ClassName: "number", Begin: `\b0[xX]((` + hexDigits + `)\.?|(` + hexDigits + `)?\.(` + hexDigits + `))[pP][+-]?(` + decimalDigits + `)[fFdD]?\b`},
			{ClassName: "number", Begin: `\b(0|[1-9](_*[0-9])*)[lL]?\b`},
			{ClassName: "number", Begin: `\b0[xX](` + hexDigits + `)[lL]?\b`},
			{ClassName: "number", Begin: `\b0(_*[0-7])*[lL]?\b`},
			{ClassName: "number", Begin: `\b0[bB][01](_*[01])*[lL]?\b`},
		},
	})
}

// javaMethodBegin is the method declaration's begin, highlight.js 10.7.3's
// `(GENERIC_IDENT_RE\s+)+UNDERSCORE_IDENT_RE\s*\(` (languages/java.js), where
// GENERIC_IDENT_RE is an identifier with an optional `<A, B>`. An identifier
// run is followed by `<` or whitespace and a whitespace run by a word, which
// neither run can take, so the repetition is taken as far as it goes and
// backtracking can only give whole repetitions back. Giving back the last one
// lets its word be the name (`int count (`); giving back more leaves a word
// that another word follows, never `(`. So the name is tried where the
// repetition ends and then where its last step began. Walked by the regexp
// this rule cost about 90 steps at every letter, most of Java's highlight
// (D523).
func javaMethodBegin(s string, pos int) (int, bool) {
	p, last, steps := pos, -1, 0
	for {
		e := javaIdentEnd(s, p)
		if e < 0 {
			break
		}
		if g := javaGenericEnd(s, e); g >= 0 && jsSpaceWidth(s, g) > 0 {
			e = g
		}
		next := skipJSSpace(s, e)
		if next == e {
			break
		}
		p, last, steps = next, p, steps+1
	}
	if steps == 0 {
		return 0, false
	}
	if end, ok := javaMethodName(s, p); ok {
		return end, true
	}
	if steps > 1 {
		return javaMethodName(s, last)
	}
	return 0, false
}

// javaMethodName matches `[a-zA-Z_]\w*\s*\(` at i.
func javaMethodName(s string, i int) (int, bool) {
	if i >= len(s) || !isWordByte(s[i]) || s[i] >= '0' && s[i] <= '9' {
		return 0, false
	}
	for i < len(s) && isWordByte(s[i]) {
		i++
	}
	if i = skipJSSpace(s, i); i < len(s) && s[i] == '(' {
		return i + 1, true
	}
	return 0, false
}

// javaGenericEnd is the end of `<IDENT(\s*,\s*IDENT)*>` at i, or -1.
func javaGenericEnd(s string, i int) int {
	if i >= len(s) || s[i] != '<' {
		return -1
	}
	i = javaIdentEnd(s, i+1)
	for i >= 0 {
		if i < len(s) && s[i] == '>' {
			return i + 1
		}
		j := skipJSSpace(s, i)
		if j >= len(s) || s[j] != ',' {
			return -1
		}
		i = javaIdentEnd(s, skipJSSpace(s, j+1))
	}
	return -1
}

// javaIdentEnd is the end of JAVA_IDENT_RE,
// `[\u00C0-\u02B8a-zA-Z_$][\u00C0-\u02B8a-zA-Z_$0-9]*`, at i, or -1.
func javaIdentEnd(s string, i int) int {
	w := javaIdentWidth(s, i)
	if w == 0 || s[i] >= '0' && s[i] <= '9' {
		return -1
	}
	for w > 0 {
		i += w
		w = javaIdentWidth(s, i)
	}
	return i
}

// javaIdentWidth is the width of an identifier character (digits included)
// at i, or 0.
func javaIdentWidth(s string, i int) int {
	if i >= len(s) {
		return 0
	}
	if c := s[i]; c < utf8.RuneSelf {
		if isWordByte(c) || c == '$' {
			return 1
		}
		return 0
	}
	if r, w := utf8.DecodeRuneInString(s[i:]); r >= 0x00C0 && r <= 0x02B8 {
		return w
	}
	return 0
}

// javaIdentFirsts is every byte JAVA_IDENT_RE can begin with: the ASCII
// letters, `_`, `$` and the UTF-8 lead bytes of U+00C0-U+02B8.
const javaIdentFirsts = "$_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz\xc3\xc4\xc5\xc6\xc7\xc8\xc9\xca"

// Java returns the Java language definition (a port of languages/java.js).
func Java() *Language {
	const javaIdentRe = `[\u00C0-\u02B8a-zA-Z_$][\u00C0-\u02B8a-zA-Z_$0-9]*`
	keywords := "false synchronized int abstract float private char boolean var static null if const " +
		"for true while long strictfp finally protected import native final void " +
		"enum else break transient catch instanceof byte super volatile case assert short " +
		"package default double public try this switch continue throws protected public private " +
		"module requires exports do"

	paren := &Mode{Begin: `\(`, End: `\)`}
	paren.Contains = []*Mode{Self}
	annotation := &Mode{
		ClassName: "meta",
		Begin:     `@` + javaIdentRe,
		Contains:  []*Mode{paren},
	}

	javadoc := Zero(Mode{
		ClassName: "comment",
		Begin:     `/\*\*`,
		End:       `\*/`,
		Contains: []*Mode{
			Zero(Mode{Begin: `\w+@`}),
			{ClassName: "doctag", Begin: `@[A-Za-z]+`},
		},
	})
	// The reference builds this with COMMENT(), which adds the phrasal-words and
	// doctag rules; the explicit javadoc contains above replace them.
	_ = HashCommentMode

	number := javaNumeric()
	numberDupe := javaNumeric()

	classMode := &Mode{
		ClassName:     "class",
		BeginKeywords: "class interface enum",
		End:           `[{;=]`,
		ExcludeEnd:    true,
		Relevance:     1,
		HasRelevance:  true,
		Keywords:      Keywords{"keyword": "class interface enum"},
		Illegal:       `[:"\[\]]`,
		Contains: []*Mode{
			{BeginKeywords: "extends implements"},
			UnderscoreTitleMode,
		},
	}
	recordMode := &Mode{
		ClassName:   "class",
		Begin:       `record\s+` + UnderscoreIdentRe + `\s*\(`,
		ReturnBegin: true,
		ExcludeEnd:  true,
		End:         `[{;=]`,
		Keywords:    Keywords{"keyword": keywords},
		Contains: []*Mode{
			{BeginKeywords: "record"},
			{Begin: UnderscoreIdentRe + `\s*\(`, ReturnBegin: true, HasRelevance: true, Relevance: 0, Contains: []*Mode{UnderscoreTitleMode}},
			{
				ClassName:    "params",
				Begin:        `\(`,
				End:          `\)`,
				Keywords:     Keywords{"keyword": keywords},
				HasRelevance: true,
				Relevance:    0,
				Contains:     []*Mode{CBlockCommentMode},
			},
			CLineCommentMode,
			CBlockCommentMode,
		},
	}
	functionMode := &Mode{
		ClassName:   "function",
		ManualBegin: javaMethodBegin,
		ManualFirst: javaIdentFirsts,
		ReturnBegin: true,
		End:         `[{;=]`,
		ExcludeEnd:  true,
		Keywords:    Keywords{"keyword": keywords},
		Contains: []*Mode{
			{Begin: UnderscoreIdentRe + `\s*\(`, ReturnBegin: true, HasRelevance: true, Relevance: 0, Contains: []*Mode{UnderscoreTitleMode}},
			{
				ClassName:    "params",
				Begin:        `\(`,
				End:          `\)`,
				Keywords:     Keywords{"keyword": keywords},
				HasRelevance: true,
				Relevance:    0,
				Contains: []*Mode{
					annotation,
					AposStringMode,
					QuoteStringMode,
					numberDupe,
					CBlockCommentMode,
				},
			},
			CLineCommentMode,
			CBlockCommentMode,
		},
	}

	importMode := &Mode{
		Begin:        `import java\.[a-z]+\.`,
		Keywords:     Keywords{"keyword": "import"},
		Relevance:    2,
		HasRelevance: true,
	}

	return &Language{
		Name:     "Java",
		Aliases:  []string{"jsp"},
		Keywords: Keywords{"keyword": keywords},
		Illegal:  `</|#`,
		Contains: []*Mode{
			javadoc,
			importMode,
			CLineCommentMode,
			CBlockCommentMode,
			AposStringMode,
			QuoteStringMode,
			classMode,
			{BeginKeywords: "new throw return else", HasRelevance: true, Relevance: 0},
			recordMode,
			functionMode,
			number,
			annotation,
		},
	}
}
