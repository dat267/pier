package highlight

func init() { RegisterLanguage("tap", TAP()) }

// TAP returns the Test Anything Protocol language definition (a port of
// languages/tap.js). A leading `---` starts a YAML block.
func TAP() *Language {
	metaBase := &Mode{ClassName: "meta"}
	metaBase.Variants = []*Mode{
		Variant(metaBase, func(m *Mode) { m.Begin = `^TAP version (\d+)$` }),
		Variant(metaBase, func(m *Mode) { m.Begin = `^1\.\.(\d+)$` }),
	}

	symbolBase := &Mode{ClassName: "symbol"}
	symbolBase.Variants = []*Mode{
		Variant(symbolBase, func(m *Mode) { m.Begin = `^ok` }),
		Variant(symbolBase, func(m *Mode) { m.Begin = `^not ok` }),
	}

	return &Language{
		Name:            "Test Anything Protocol",
		CaseInsensitive: true,
		Contains: []*Mode{
			HashCommentMode,
			metaBase,
			{Begin: `---$`, End: `\.\.\.$`, SubLanguage: []string{"yaml"}, HasRelevance: true, Relevance: 0},
			{ClassName: "number", Begin: ` (\d+) `},
			symbolBase,
		},
	}
}
