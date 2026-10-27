package highlight

import "strings"

func init() { RegisterLanguage("smali", Smali()) }

// Smali returns the Smali language definition (a port of languages/smali.js).
func Smali() *Language {
	lowPrio := []string{
		"add", "and", "cmp", "cmpg", "cmpl", "const", "div", "double", "float",
		"goto", "if", "int", "long", "move", "mul", "neg", "new", "nop", "not",
		"or", "rem", "return", "shl", "shr", "sput", "sub", "throw", "ushr", "xor",
	}
	highPrio := []string{
		"aget", "aput", "array", "check", "execute", "fill", "filled", "goto/16",
		"goto/32", "iget", "instance", "invoke", "iput", "monitor", "packed",
		"sget", "sparse",
	}
	keywords := []string{
		"transient", "constructor", "abstract", "final", "synthetic", "public",
		"private", "protected", "static", "bridge", "system",
	}

	comment := CommentMode(`#`, `$`)
	comment.HasRelevance = true
	comment.Relevance = 0

	keyword := &Mode{ClassName: "keyword"}
	keyword.Variants = []*Mode{
		Variant(keyword, func(m *Mode) { m.Begin = `\s*\.end\s[a-zA-Z0-9]*` }),
		Variant(keyword, func(m *Mode) { m.Begin = `^[ ]*\.[a-zA-Z]*`; m.HasRelevance = true; m.Relevance = 0 }),
		Variant(keyword, func(m *Mode) { m.Begin = `\s:[a-zA-Z_0-9]*`; m.HasRelevance = true; m.Relevance = 0 }),
		Variant(keyword, func(m *Mode) { m.Begin = `\s(` + strings.Join(keywords, "|") + `)` }),
	}

	builtIn := &Mode{ClassName: "built_in"}
	builtIn.Variants = []*Mode{
		Variant(builtIn, func(m *Mode) { m.Begin = `\s(` + strings.Join(lowPrio, "|") + `)\s` }),
		Variant(builtIn, func(m *Mode) {
			m.Begin = `\s(` + strings.Join(lowPrio, "|") + `)((-|/)[a-zA-Z0-9]+)+\s`
			m.HasRelevance = true
			m.Relevance = 10
		}),
		Variant(builtIn, func(m *Mode) {
			m.Begin = `\s(` + strings.Join(highPrio, "|") + `)((-|/)[a-zA-Z0-9]+)*\s`
			m.HasRelevance = true
			m.Relevance = 10
		}),
	}

	return &Language{
		Name: "Smali",
		Contains: []*Mode{
			{ClassName: "string", Begin: `"`, End: `"`, HasRelevance: true, Relevance: 0},
			comment,
			keyword,
			builtIn,
			{ClassName: "class", Begin: `L[^\(;:\n]*;`, HasRelevance: true, Relevance: 0},
			{Begin: `[vp][0-9]+`},
		},
	}
}
