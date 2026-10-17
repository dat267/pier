package highlight

func init() { RegisterLanguage("diff", Diff()) }

// Diff returns the Diff language definition (a port of languages/diff.js).
// The meta and comment groups are variant sets, so each variant inherits the
// parent's class and relevance through Variant.
func Diff() *Language {
	meta := &Mode{ClassName: "meta", HasRelevance: true, Relevance: 10}
	meta.Variants = []*Mode{
		Variant(meta, func(m *Mode) { m.Begin = `^@@ +-\d+,\d+ +\+\d+,\d+ +@@` }),
		Variant(meta, func(m *Mode) { m.Begin = `^\*\*\* +\d+,\d+ +\*\*\*\*$` }),
		Variant(meta, func(m *Mode) { m.Begin = `^--- +\d+,\d+ +----$` }),
	}

	comment := &Mode{ClassName: "comment"}
	comment.Variants = []*Mode{
		Variant(comment, func(m *Mode) { m.Begin = `Index: `; m.End = `$` }),
		Variant(comment, func(m *Mode) { m.Begin = `^index`; m.End = `$` }),
		Variant(comment, func(m *Mode) { m.Begin = `={3,}`; m.End = `$` }),
		Variant(comment, func(m *Mode) { m.Begin = `^-{3}`; m.End = `$` }),
		Variant(comment, func(m *Mode) { m.Begin = `^\*{3} `; m.End = `$` }),
		Variant(comment, func(m *Mode) { m.Begin = `^\+{3}`; m.End = `$` }),
		Variant(comment, func(m *Mode) { m.Begin = `^\*{15}$` }),
		Variant(comment, func(m *Mode) { m.Begin = `^diff --git`; m.End = `$` }),
	}

	return &Language{
		Name:    "Diff",
		Aliases: []string{"patch"},
		Mode: Mode{
			Contains: []*Mode{
				meta,
				comment,
				{ClassName: "addition", Begin: `^\+`, End: `$`},
				{ClassName: "deletion", Begin: `^-`, End: `$`},
				{ClassName: "addition", Begin: `^!`, End: `$`},
			},
		},
	}
}
