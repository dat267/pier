package highlight

func init() { RegisterLanguage("prolog", Prolog()) }

// Prolog returns the Prolog language definition (a port of languages/prolog.js).
func Prolog() *Language {
	atom := &Mode{Begin: `[a-z][A-Za-z0-9_]*`, HasRelevance: true, Relevance: 0}

	varBase := &Mode{ClassName: "symbol", HasRelevance: true, Relevance: 0}
	varBase.Variants = []*Mode{
		Variant(varBase, func(m *Mode) { m.Begin = `[A-Z][a-zA-Z0-9_]*` }),
		Variant(varBase, func(m *Mode) { m.Begin = `_[A-Za-z0-9_]*` }),
	}

	parented := &Mode{Begin: `\(`, End: `\)`, HasRelevance: true, Relevance: 0}
	list := &Mode{Begin: `\[`, End: `\]`}
	lineComment := &Mode{
		ClassName: "comment",
		Begin:     `%`,
		End:       `$`,
		Contains:  []*Mode{PhrasalWordsMode},
	}
	backtick := &Mode{ClassName: "string", Begin: "\x60", End: "\x60", Contains: []*Mode{BackslashEscape}}
	charCode := &Mode{ClassName: "string", Begin: `0'(\\'|.)`}
	spaceCode := &Mode{ClassName: "string", Begin: `0'\\s`}
	predOp := &Mode{Begin: `:-`}

	inner := []*Mode{
		atom,
		varBase,
		parented,
		predOp,
		list,
		lineComment,
		CBlockCommentMode,
		QuoteStringMode,
		AposStringMode,
		backtick,
		charCode,
		spaceCode,
		CNumberMode,
	}
	parented.Contains = inner
	list.Contains = inner

	root := append([]*Mode{}, inner...)
	root = append(root, &Mode{Begin: `\.$`})

	return &Language{Name: "Prolog", Contains: root}
}
