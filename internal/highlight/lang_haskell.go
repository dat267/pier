package highlight

func init() { RegisterLanguage("haskell", Haskell()) }

// Haskell returns the Haskell language definition (a port of
// languages/haskell.js).
func Haskell() *Language {
	commentDash := CommentMode(`--`, `$`)
	commentBlock := CommentMode(`\{-`, `-\}`)
	commentBlock.Contains = append([]*Mode{Self}, commentBlock.Contains...)
	comment := &Mode{Variants: []*Mode{commentDash, commentBlock}}

	pragma := &Mode{ClassName: "meta", Begin: `\{-#`, End: `#-\}`}
	preprocessor := &Mode{ClassName: "meta", Begin: `^#`, End: `$`}
	constructor := &Mode{ClassName: "type", Begin: `\b[A-Z][\w']*`, HasRelevance: true, Relevance: 0}
	listTitle := Variant(TitleMode, func(m *Mode) { m.Begin = `[_a-z][\w']*` })
	rootTitle := Variant(TitleMode, func(m *Mode) { m.Begin = `^[_a-z][\w']*` })

	list := &Mode{
		Begin:   `\(`,
		End:     `\)`,
		Illegal: `"`,
		Contains: []*Mode{
			pragma,
			preprocessor,
			{ClassName: "type", Begin: `\b[A-Z][\w]*(\(\.\.|,|\w+\))?`},
			listTitle,
			comment,
		},
	}
	record := &Mode{Begin: `\{`, End: `\}`, Contains: list.Contains}

	return &Language{
		Name:    "Haskell",
		Aliases: []string{"hs"},
		Keywords: Keywords{
			"keyword": "let in if then else case of where do module import hiding qualified type data newtype deriving class " +
				"instance as default infix infixl infixr foreign export ccall stdcall cplusplus jvm dotnet safe " +
				"unsafe family forall mdo proc rec",
		},
		Contains: []*Mode{
			{
				BeginKeywords: "module",
				End:           `where`,
				Keywords:      Keywords{"keyword": "module where"},
				Contains:      []*Mode{list, comment},
				Illegal:       `\W\.|;`,
			},
			{
				Begin:    `\bimport\b`,
				End:      `$`,
				Keywords: Keywords{"keyword": "import qualified as hiding"},
				Contains: []*Mode{list, comment},
				Illegal:  `\W\.|;`,
			},
			{
				ClassName: "class",
				Begin:     `^(\s*)?(class|instance)\b`,
				End:       `where`,
				Keywords:  Keywords{"keyword": "class family instance where"},
				Contains:  []*Mode{constructor, list, comment},
			},
			{
				ClassName: "class",
				Begin:     `\b(data|(new)?type)\b`,
				End:       `$`,
				Keywords:  Keywords{"keyword": "data family type newtype deriving"},
				Contains:  []*Mode{pragma, constructor, list, record, comment},
			},
			{BeginKeywords: "default", End: `$`, Contains: []*Mode{constructor, list, comment}},
			{
				BeginKeywords: "infix infixl infixr",
				End:           `$`,
				Contains:      []*Mode{CNumberMode, comment},
			},
			{
				Begin:    `\bforeign\b`,
				End:      `$`,
				Keywords: Keywords{"keyword": "foreign import export ccall stdcall cplusplus jvm dotnet safe unsafe"},
				Contains: []*Mode{constructor, QuoteStringMode, comment},
			},
			{ClassName: "meta", Begin: `#!\/usr\/bin\/env runhaskell`, End: `$`},
			pragma,
			preprocessor,
			QuoteStringMode,
			CNumberMode,
			constructor,
			rootTitle,
			comment,
			{Begin: `->|<-|=>`},
		},
	}
}
