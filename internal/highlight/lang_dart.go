package highlight

import "strings"

func init() { RegisterLanguage("dart", Dart()) }

// Dart returns the Dart language definition (a port of languages/dart.js).
func Dart() *Language {
	subst := &Mode{
		ClassName: "subst",
		Variants:  []*Mode{{ClassName: "subst", Begin: `\$[A-Za-z0-9_]+`}},
	}
	bracedSubst := &Mode{
		ClassName: "subst",
		Variants:  []*Mode{{ClassName: "subst", Begin: `\$\{`, End: `\}`}},
		Keywords:  Keywords{"keyword": "true false null this is new super"},
	}

	stringMode := &Mode{
		ClassName: "string",
		Variants: []*Mode{
			{ClassName: "string", Begin: `r'''`, End: `'''`},
			{ClassName: "string", Begin: `r"""`, End: `"""`},
			{ClassName: "string", Begin: `r'`, End: `'`, Illegal: `\n`},
			{ClassName: "string", Begin: `r"`, End: `"`, Illegal: `\n`},
			{ClassName: "string", Begin: `'''`, End: `'''`, Contains: []*Mode{BackslashEscape, subst, bracedSubst}},
			{ClassName: "string", Begin: `"""`, End: `"""`, Contains: []*Mode{BackslashEscape, subst, bracedSubst}},
			{ClassName: "string", Begin: `'`, End: `'`, Illegal: `\n`, Contains: []*Mode{BackslashEscape, subst, bracedSubst}},
			{ClassName: "string", Begin: `"`, End: `"`, Illegal: `\n`, Contains: []*Mode{BackslashEscape, subst, bracedSubst}},
		},
	}
	bracedSubst.Contains = []*Mode{CNumberMode, stringMode}

	builtInTypes := []string{"Comparable", "DateTime", "Duration", "Function", "Iterable", "Iterator", "List", "Map", "Match", "Object", "Pattern", "RegExp", "Set", "Stopwatch", "String", "StringBuffer", "StringSink", "Symbol", "Type", "Uri", "bool", "double", "int", "num", "Element", "ElementList"}
	nullable := make([]string, len(builtInTypes))
	for i, t := range builtInTypes {
		nullable[i] = t + "?"
	}
	builtIns := append(append(append([]string{}, builtInTypes...), nullable...),
		"Never", "Null", "dynamic", "print", "document", "querySelector", "querySelectorAll", "window")

	docComment := commentMode(`/\*\*`, `\*/`)
	docComment.HasRelevance = true
	docComment.Relevance = 0
	docComment.SubLanguage = []string{"markdown"}
	docComment.beforeBegin = func(m *match, resp *Response) {
		if next := m.index + len(m.lexeme); next < len(m.input) && m.input[next] == '/' {
			resp.IgnoreMatch()
		}
	}

	tripleSlash := commentMode(`/{3,} ?`, `$`)
	tripleSlash.Contains = append([]*Mode{
		Zero(Mode{Begin: `.`, End: `$`, SubLanguage: []string{"markdown"}}),
	}, PhrasalWordsMode, Zero(Mode{ClassName: "doctag", Begin: `(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):`}))

	classMode := &Mode{
		ClassName:     "class",
		BeginKeywords: "class interface",
		End:           `\{`,
		ExcludeEnd:    true,
		Contains: []*Mode{
			{BeginKeywords: "extends implements"},
			UnderscoreTitleMode,
		},
	}

	return &Language{
		Name: "Dart",
		Keywords: Keywords{
			"$pattern": `[A-Za-z][A-Za-z0-9_]*\??`,
			"keyword": "abstract as assert async await break case catch class const continue covariant default deferred do " +
				"dynamic else enum export extends extension external factory false final finally for Function get hide if " +
				"implements import in inferface is late library mixin new null on operator part required rethrow return set " +
				"show static super switch sync this throw true try typedef var void while with yield",
			"built_in": joinSpace(builtIns),
		},
		Contains: []*Mode{
			stringMode,
			docComment,
			tripleSlash,
			CLineCommentMode,
			CBlockCommentMode,
			classMode,
			CNumberMode,
			{ClassName: "meta", Begin: `@[A-Za-z]+`},
			{Begin: `=>`},
		},
	}
}

func joinSpace(values []string) string {
	var out strings.Builder
	for i, v := range values {
		if i > 0 {
			out.WriteString(" ")
		}
		out.WriteString(v)
	}
	return out.String()
}
