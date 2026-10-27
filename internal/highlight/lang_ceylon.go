package highlight

func init() { RegisterLanguage("ceylon", Ceylon()) }

// Ceylon returns the Ceylon language definition (a port of languages/ceylon.js).
func Ceylon() *Language {
	const keywords = "assembly module package import alias class interface object given value " +
		"assign void function new of extends satisfies abstracts in out return " +
		"break continue throw assert dynamic if else switch case for while try " +
		"catch finally then let this outer super is exists nonempty"
	const modifiers = "shared abstract formal default actual variable late native deprecated " +
		"final sealed annotation suppressWarnings small"
	const documentation = "doc by license see throws tagged"

	subst := &Mode{
		ClassName:    "subst",
		ExcludeBegin: true,
		ExcludeEnd:   true,
		Begin:        "\x60\x60",
		End:          "\x60\x60",
		Keywords:     Keywords{"keyword": keywords},
		HasRelevance: true,
		Relevance:    10,
	}
	expressions := []*Mode{
		{ClassName: "string", Begin: `"""`, End: `"""`, HasRelevance: true, Relevance: 10},
		{ClassName: "string", Begin: `"`, End: `"`, Contains: []*Mode{subst}},
		{ClassName: "string", Begin: `'`, End: `'`},
		{
			ClassName:    "number",
			Begin:        `#[0-9a-fA-F_]+|\$[01_]+|[0-9_]+(?:\.[0-9_](?:[eE][+-]?\d+)?)?[kMGTPmunpf]?`,
			HasRelevance: true,
			Relevance:    0,
		},
	}
	subst.Contains = expressions

	blockComment := CommentMode(`/\*`, `\*/`)
	blockComment.Contains = append([]*Mode{Self}, blockComment.Contains...)

	return &Language{
		Name: "Ceylon",
		Keywords: Keywords{
			"keyword": keywords + " " + modifiers,
			"meta":    documentation,
		},
		Illegal: `\$[^01]|#[^0-9a-fA-F]`,
		Contains: append([]*Mode{
			CLineCommentMode,
			blockComment,
			{ClassName: "meta", Begin: `@[a-z]\w*(?::"[^"]*")?`},
		}, expressions...),
	}
}
