package highlight

func init() { RegisterLanguage("vbnet", VBNet()) }

// VBNet returns the Visual Basic .NET language definition (a port of
// languages/vbnet.js).
func VBNet() *Language {
	character := &Mode{ClassName: "string", Begin: `"(""|[^/n])"C\b`}
	stringMode := &Mode{ClassName: "string", Begin: `"`, End: `"`, Illegal: `\n`, Contains: []*Mode{{Begin: `""`}}}

	dateBase := &Mode{ClassName: "literal"}
	dateBase.Variants = []*Mode{
		Variant(dateBase, func(m *Mode) { m.Begin = `# *(\d{4}-\d{1,2}-\d{1,2}|\d{1,2}\/\d{1,2}\/\d{4}) *#` }),
		Variant(dateBase, func(m *Mode) { m.Begin = `# *\d{1,2}(:\d{1,2}){1,2} *#` }),
		Variant(dateBase, func(m *Mode) { m.Begin = `# *(\d|1[012])(:\d+){0,2} *(AM|PM) *#` }),
		Variant(dateBase, func(m *Mode) {
			m.Begin = `# *(\d{4}-\d{1,2}-\d{1,2}|\d{1,2}\/\d{1,2}\/\d{4}) +((\d|1[012])(:\d+){0,2} *(AM|PM)|\d{1,2}(:\d{1,2}){1,2}) *#`
		}),
	}

	numberBase := &Mode{ClassName: "number", HasRelevance: true, Relevance: 0}
	numberBase.Variants = []*Mode{
		Variant(numberBase, func(m *Mode) { m.Begin = `\b\d[\d_]*((\.[\d_]+(E[+-]?[\d_]+)?)|(E[+-]?[\d_]+))[RFD@!#]?` }),
		Variant(numberBase, func(m *Mode) { m.Begin = `\b\d[\d_]*((U?[SIL])|[%&])?` }),
		Variant(numberBase, func(m *Mode) { m.Begin = `&H[\dA-F_]+((U?[SIL])|[%&])?` }),
		Variant(numberBase, func(m *Mode) { m.Begin = `&O[0-7_]+((U?[SIL])|[%&])?` }),
		Variant(numberBase, func(m *Mode) { m.Begin = `&B[01_]+((U?[SIL])|[%&])?` }),
	}

	label := &Mode{ClassName: "label", Begin: `^\w+:`}

	docComment := CommentMode(`'''`, `$`)
	docComment.Contains = append([]*Mode{{ClassName: "doctag", Begin: `<\/?`, End: `>`}}, docComment.Contains...)

	commentBase := CommentMode(``, `$`)
	remVariant := Variant(commentBase, func(m *Mode) { m.Begin = `([\t ]|^)REM` })
	remVariant.beforeBegin = func(m *match, resp *Response) {
		if end := m.index + len(m.lexeme); end >= len(m.input) || !isWhitespaceByte(m.input[end]) {
			resp.IgnoreMatch()
		}
	}
	commentBase.Variants = []*Mode{
		Variant(commentBase, func(m *Mode) { m.Begin = `'` }),
		remVariant,
	}

	directives := &Mode{
		ClassName: "meta",
		Begin:     `[\t ]*#(const|disable|else|elseif|enable|end|externalsource|if|region)\b`,
		End:       `$`,
		Keywords:  Keywords{"meta-keyword": "const disable else elseif enable end externalsource if region then"},
		Contains:  []*Mode{commentBase},
	}

	return &Language{
		Name:             "Visual Basic .NET",
		Aliases:          []string{"vb"},
		CaseInsensitive:  true,
		ClassNameAliases: map[string]string{"label": "symbol"},
		Mode: Mode{
			Keywords: Keywords{
				"keyword": "addhandler alias aggregate ansi as async assembly auto binary by byref byval call case catch class " +
					"compare const continue custom declare default delegate dim distinct do each equals else elseif end " +
					"enum erase error event exit explicit finally for friend from function get global goto group handles " +
					"if implements imports in inherits interface into iterator join key let lib loop me mid module " +
					"mustinherit mustoverride mybase myclass namespace narrowing new next notinheritable notoverridable " +
					"of off on operator option optional order overloads overridable overrides paramarray partial preserve " +
					"private property protected public raiseevent readonly redim removehandler resume return select set " +
					"shadows shared skip static step stop structure strict sub synclock take text then throw to try " +
					"unicode until using when where while widening with withevents writeonly yield",
				"built_in": "addressof and andalso await directcast gettype getxmlnamespace is isfalse isnot istrue like mod " +
					"nameof new not or orelse trycast typeof xor cbool cbyte cchar cdate cdbl cdec cint clng cobj csbyte " +
					"cshort csng cstr cuint culng cushort",
				"type": "boolean byte char date decimal double integer long object sbyte short single string uinteger ulong " +
					"ushort",
				"literal": "true false nothing",
			},
			Illegal: `//|\{|\}|endif|gosub|variant|wend|^\$ `,
			Contains: []*Mode{
				character,
				stringMode,
				dateBase,
				numberBase,
				label,
				docComment,
				commentBase,
				directives,
			},
		},
	}
}
