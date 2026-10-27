package highlight

func init() { RegisterLanguage("haxe", Haxe()) }

// Haxe returns the Haxe language definition (a port of languages/haxe.js).
func Haxe() *Language {
	singleString := &Mode{
		ClassName: "string",
		Begin:     `'`,
		End:       `'`,
		Contains: []*Mode{
			BackslashEscape,
			{ClassName: "subst", Begin: `\$\{`, End: `\}`},
			{ClassName: "subst", Begin: `\$`, End: `\W\}`},
		},
	}
	abstractClass := &Mode{
		ClassName:     "class",
		BeginKeywords: "abstract",
		End:           `[\{$]`,
		Keywords:      Keywords{"keyword": "abstract from to"},
		Contains: []*Mode{
			{ClassName: "type", Begin: `\(`, End: `\)`, ExcludeBegin: true, ExcludeEnd: true},
			{ClassName: "type", Begin: `from +`, End: `\W`, ExcludeBegin: true, ExcludeEnd: true},
			{ClassName: "type", Begin: `to +`, End: `\W`, ExcludeBegin: true, ExcludeEnd: true},
			TitleMode,
		},
	}
	classMode := &Mode{
		ClassName:  "class",
		Begin:      `\b(class|interface) +`,
		End:        `[\{$]`,
		ExcludeEnd: true,
		Keywords:   Keywords{"keyword": "class interface"},
		Contains: []*Mode{
			{
				ClassName: "keyword",
				Begin:     `\b(extends|implements) +`,
				Keywords:  Keywords{"keyword": "extends implements"},
				Contains: []*Mode{
					{ClassName: "type", Begin: IdentRe, HasRelevance: true, Relevance: 0},
				},
			},
			TitleMode,
		},
	}
	return &Language{
		Name:    "Haxe",
		Aliases: []string{"hx"},
		Keywords: Keywords{
			"keyword": "break case cast catch continue default do dynamic else enum extern for function here if import in " +
				"inline never new override package private get set public return static super switch this throw trace " +
				"try typedef untyped using var while Int Float String Bool Dynamic Void Array ",
			"built_in": "trace this",
			"literal":  "true false null _",
		},
		Illegal: `<//`,
		Contains: []*Mode{
			singleString,
			QuoteStringMode,
			CLineCommentMode,
			CBlockCommentMode,
			CNumberMode,
			{ClassName: "meta", Begin: `@:`, End: `$`},
			{
				ClassName: "meta",
				Begin:     `#`,
				End:       `$`,
				Keywords:  Keywords{"meta-keyword": "if else elseif end error"},
			},
			{
				ClassName:    "type",
				Begin:        `:[ \t]*`,
				End:          `[^A-Za-z0-9_ \t\->]`,
				ExcludeBegin: true,
				ExcludeEnd:   true,
				HasRelevance: true,
				Relevance:    0,
			},
			{ClassName: "type", Begin: `:[ \t]*`, End: `\W`, ExcludeBegin: true, ExcludeEnd: true},
			{ClassName: "type", Begin: `new *`, End: `\W`, ExcludeBegin: true, ExcludeEnd: true},
			{ClassName: "class", BeginKeywords: "enum", End: `\{`, Contains: []*Mode{TitleMode}},
			abstractClass,
			classMode,
			{
				ClassName:     "function",
				BeginKeywords: "function",
				End:           `\(`,
				ExcludeEnd:    true,
				Illegal:       `\S`,
				Contains:      []*Mode{TitleMode},
			},
		},
	}
}
