package highlight

import (
	"regexp"
	"strings"
)

func init() { RegisterLanguage("swift", Swift()) }

const (
	swiftIdentifierHead    = `[a-zA-Z_\u00A8\u00AA\u00AD\u00AF\u00B2-\u00B5\u00B7-\u00BA\u00BC-\u00BE\u00C0-\u00D6\u00D8-\u00F6\u00F8-\u00FF\u0100-\u02FF\u0370-\u167F\u1681-\u180D\u180F-\u1DBF\u1E00-\u1FFF\u200B-\u200D\u202A-\u202E\u203F-\u2040\u2054\u2060-\u206F\u2070-\u20CF\u2100-\u218F\u2460-\u24FF\u2776-\u2793\u2C00-\u2DFF\u2E80-\u2FFF\u3004-\u3007\u3021-\u302F\u3031-\u303F\u3040-\uD7FF\uF900-\uFD3D\uFD40-\uFDCF\uFDF0-\uFE1F\uFE30-\uFE44\uFE47-\uFEFE\uFF00-\uFFFD]`
	swiftIdentifierChar    = `(` + swiftIdentifierHead + `|\d|[\u0300-\u036F\u1DC0-\u1DFF\u20D0-\u20FF\uFE20-\uFE2F])`
	swiftIdentifier        = swiftIdentifierHead + swiftIdentifierChar + `*`
	swiftTypeIdentifier    = `[A-Z]` + swiftIdentifierChar + `*`
	swiftOperatorHead      = `([/=\-+!*%<>&|^~?]|[\u00A1-\u00A7]|[\u00A9\u00AB]|[\u00AC\u00AE]|[\u00B0\u00B1]|[\u00B6\u00BB\u00BF\u00D7\u00F7]|[\u2016-\u2017]|[\u2020-\u2027]|[\u2030-\u203E]|[\u2041-\u2053]|[\u2055-\u205E]|[\u2190-\u23FF]|[\u2500-\u2775]|[\u2794-\u2BFF]|[\u2E00-\u2E7F]|[\u3001-\u3003]|[\u3008-\u3020]|[\u3030])`
	swiftOperatorCharacter = `(` + swiftOperatorHead + `|[\u0300-\u036F]|[\u1DC0-\u1DFF]|[\u20D0-\u20FF]|[\uFE00-\uFE0F]|[\uFE20-\uFE2F])`
	swiftOperator          = swiftOperatorHead + swiftOperatorCharacter + `*`
	swiftDecimalDigits     = `([0-9]_*)+`
	swiftHexDigits         = `([0-9a-fA-F]_*)+`
)

var swiftKeywords = []string{
	"associatedtype", "async", "await", "as", "break", "case", "catch", "class",
	"continue", "convenience", "default", "defer", "deinit", "didSet", "do",
	"dynamic", "else", "enum", "extension", "fallthrough", "fileprivate", "final",
	"for", "func", "get", "guard", "if", "import", "indirect", "infix", "inout",
	"internal", "in", "is", "lazy", "let", "mutating", "nonmutating", "open",
	"operator", "optional", "override", "postfix", "precedencegroup", "prefix",
	"private", "protocol", "public", "repeat", "required", "rethrows", "return",
	"set", "some", "static", "struct", "subscript", "super", "switch", "throws",
	"throw", "try", "typealias", "unowned", "var", "weak", "where", "while", "willSet",
}

var swiftRegexKeywords = []string{
	`as\?`, `as!`, `fileprivate\(set\)`, `init\?`, `init!`, `internal\(set\)`,
	`open\(set\)`, `private\(set\)`, `public\(set\)`, `try\?`, `try!`,
	`unowned\(safe\)`, `unowned\(unsafe\)`,
}

var swiftNumberSignKeywords = []string{
	"#colorLiteral", "#column", "#dsohandle", "#else", "#elseif", "#endif",
	"#error", "#file", "#fileID", "#fileLiteral", "#filePath", "#function",
	"#if", "#imageLiteral", "#keyPath", "#line", "#selector", "#sourceLocation",
	"#warn_unqualified_access", "#warning",
}

var swiftLiterals = []string{"false", "nil", "true"}

var swiftPrecedencegroupKeywords = []string{"assignment", "associativity", "higherThan", "left", "lowerThan", "none", "right"}

var swiftBuiltIns = []string{
	"abs", "all", "any", "assert", "assertionFailure", "debugPrint", "dump",
	"fatalError", "getVaList", "isKnownUniquelyReferenced", "max", "min",
	"numericCast", "pointwiseMax", "pointwiseMin", "precondition",
	"preconditionFailure", "print", "readLine", "repeatElement", "sequence",
	"stride", "swap", "swift_unboxFromSwiftValueWithType", "transcode", "type",
	"unsafeBitCast", "unsafeDowncast", "withExtendedLifetime",
	"withUnsafeMutablePointer", "withUnsafePointer", "withVaList",
	"withoutActuallyEscaping", "zip",
}

var swiftKeywordAttributes = []string{
	"autoclosure", `convention\(` + `(swift|block|c)` + `\)`, "discardableResult",
	"dynamicCallable", "dynamicMemberLookup", "escaping", "frozen", "GKInspectable",
	"IBAction", "IBDesignable", "IBInspectable", "IBOutlet", "IBSegueAction",
	"inlinable", "main", "nonobjc", "NSApplicationMain", "NSCopying", "NSManaged",
	`objc\(` + swiftIdentifier + `\)`, "objc", "objcMembers", "propertyWrapper",
	"requires_stored_property_inits", "testable", "UIApplicationMain", "unknown",
	"usableFromInline",
}

var swiftAvailabilityKeywords = []string{
	"iOS", "iOSApplicationExtension", "macOS", "macOSApplicationExtension",
	"macCatalyst", "macCatalystApplicationExtension", "watchOS",
	"watchOSApplicationExtension", "tvOS", "tvOSApplicationExtension", "swift",
}

func swiftKeywordWrapper(kw string, wordEnd bool) string {
	suffix := `\B`
	if wordEnd {
		suffix = `\b`
	}
	return `\b` + kw + suffix
}

func eitherJoin(parts []string) string { return "(" + strings.Join(parts, "|") + ")" }

// Swift returns the Swift language definition (a port of languages/swift.js).
func Swift() *Language {
	dotKeywords := []string{swiftKeywordWrapper("Protocol", true), swiftKeywordWrapper("Type", true)}
	optionalDotKeywords := []string{swiftKeywordWrapper("init", true), swiftKeywordWrapper("self", true)}
	keywordTypes := []string{"Any", "Self"}

	// REGEX_KEYWORDS = regex keywords + keywordTypes, wrapped.
	var regexKeywords []string
	for _, kw := range swiftRegexKeywords {
		regexKeywords = append(regexKeywords, swiftKeywordWrapper(kw, false))
	}
	for _, kw := range keywordTypes {
		regexKeywords = append(regexKeywords, swiftKeywordWrapper(kw, true))
	}

	keywords := Keywords{
		"$pattern": eitherJoin([]string{`\b\w+`, `#\w+`}),
		"keyword":  strings.Join(append(append([]string{}, swiftKeywords...), swiftNumberSignKeywords...), " "),
		"literal":  strings.Join(swiftLiterals, " "),
	}

	docKeywords := append(append([]string{}, dotKeywords...), optionalDotKeywords...)
	allKeywordSources := append([]string{}, swiftKeywords...)
	allKeywordSources = append(allKeywordSources, swiftRegexKeywords...)

	whitespace := Zero(Mode{Match: `\s+`})

	blockComment := commentMode(`/\*`, `\*/`)
	blockComment.Contains = append(blockComment.Contains, Self)
	comments := []*Mode{CLineCommentMode, blockComment}

	dotKeyword := &Mode{
		ClassName:    "keyword",
		Begin:        `\.`,
		End:          eitherJoin(docKeywords),
		ExcludeBegin: true,
	}
	dotKeyword.beforeBegin = func(m *match, resp *Response) {
		if !swiftDotKeywordGuard.MatchString(m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}
	keywordGuard := Zero(Mode{Match: `\.` + eitherJoin(allKeywordSources)})
	keyword := &Mode{
		Variants: []*Mode{{ClassName: "keyword", Match: eitherJoin(append(append([]string{}, regexKeywords...), optionalDotKeywords...))}},
	}
	keywordModes := []*Mode{dotKeyword, keywordGuard, keyword}

	builtInGuard := Zero(Mode{Match: `\.` + eitherJoin(swiftBuiltIns)})
	builtIn := &Mode{ClassName: "built_in", Match: `\b` + eitherJoin(swiftBuiltIns)}
	builtIn.beforeBegin = func(m *match, resp *Response) {
		next := m.index + len(m.lexeme)
		if next >= len(m.input) || m.input[next] != '(' {
			resp.IgnoreMatch()
		}
	}
	builtIns := []*Mode{builtInGuard, builtIn}

	operatorGuard := Zero(Mode{Match: `->`})
	operator := &Mode{
		ClassName:    "operator",
		HasRelevance: true,
		Relevance:    0,
		Variants: []*Mode{
			Zero(Mode{ClassName: "operator", Match: swiftOperator}),
			Zero(Mode{ClassName: "operator", Match: `\.(\.|` + swiftOperatorCharacter + `)+`}),
		},
	}
	operators := []*Mode{operatorGuard, operator}

	number := Zero(Mode{
		ClassName: "number",
		Variants: []*Mode{
			{ClassName: "number", Match: `\b(` + swiftDecimalDigits + `)(\.(` + swiftDecimalDigits + `))?([eE][+-]?(` + swiftDecimalDigits + `))?\b`},
			{ClassName: "number", Match: `\b0x(` + swiftHexDigits + `)(\.(` + swiftHexDigits + `))?([pP][+-]?(` + swiftDecimalDigits + `))?\b`},
			{ClassName: "number", Match: `\b0o([0-7]_*)+\b`},
			{ClassName: "number", Match: `\b0b([01]_*)+\b`},
		},
	})

	escapedCharacter := func(rd string) *Mode {
		return Zero(Mode{
			ClassName: "subst",
			Variants: []*Mode{
				{ClassName: "subst", Match: `\\` + rd + `[0\\tnr"']`},
				{ClassName: "subst", Match: `\\` + rd + `u\{[0-9a-fA-F]{1,8}\}`},
			},
		})
	}
	escapedNewline := func(rd string) *Mode {
		return Zero(Mode{ClassName: "subst", Match: `\\` + rd + `[\t ]*(?:\r\n|\r|\n)`})
	}
	// The interpolation mode is built per raw delimiter; its contains are filled
	// in after the submode list exists (the reference's post-processing loop).
	interpolationFor := func(rd string) *Mode {
		return &Mode{ClassName: "subst", Begin: `\\` + rd + `\(`, End: `\)`}
	}
	rawDelimiters := []string{"", "#", "##", "###"}
	escapedByRD := map[string]*Mode{}
	newlineByRD := map[string]*Mode{}
	interpolationByRD := map[string]*Mode{}
	for _, rd := range rawDelimiters {
		escapedByRD[rd] = escapedCharacter(rd)
		newlineByRD[rd] = escapedNewline(rd)
		interpolationByRD[rd] = interpolationFor(rd)
	}
	multilineString := func(rd string) *Mode {
		return &Mode{
			ClassName: "string",
			Begin:     rd + `"""`,
			End:       `"""` + rd,
			Contains:  []*Mode{escapedByRD[rd], newlineByRD[rd], interpolationByRD[rd]},
		}
	}
	singleLineString := func(rd string) *Mode {
		return &Mode{
			ClassName: "string",
			Begin:     rd + `"`,
			End:       `"` + rd,
			Contains:  []*Mode{escapedByRD[rd], interpolationByRD[rd]},
		}
	}
	stringMode := &Mode{
		ClassName: "string",
		Variants: []*Mode{
			multilineString(""), multilineString("#"), multilineString("##"), multilineString("###"),
			singleLineString(""), singleLineString("#"), singleLineString("##"), singleLineString("###"),
		},
	}

	quotedIdentifier := &Mode{Match: "`" + swiftIdentifier + "`"}
	implicitParameter := &Mode{ClassName: "variable", Match: `\$\d+`}
	propertyWrapperProjection := &Mode{ClassName: "variable", Match: `\$` + swiftIdentifierChar + `+`}
	identifiers := []*Mode{quotedIdentifier, implicitParameter, propertyWrapperProjection}

	// Interpolation submodes (the reference's post-processing loop).
	var interpolationSubmodes []*Mode
	interpolationSubmodes = append(interpolationSubmodes, keywordModes...)
	interpolationSubmodes = append(interpolationSubmodes, builtIns...)
	interpolationSubmodes = append(interpolationSubmodes, operators...)
	interpolationSubmodes = append(interpolationSubmodes, number)
	interpolationSubmodes = append(interpolationSubmodes, stringMode)
	interpolationSubmodes = append(interpolationSubmodes, identifiers...)
	for _, rd := range rawDelimiters {
		m := interpolationByRD[rd]
		m.Keywords = keywords
		interpolationParen := &Mode{Begin: `\(`, End: `\)`}
		interpolationParen.Contains = append([]*Mode{Self}, interpolationSubmodes...)
		m.Contains = append(append([]*Mode{}, interpolationSubmodes...), interpolationParen)
	}

	availableAttribute := &Mode{
		ClassName: "keyword",
		Match:     `(@|#)available`,
		Starts: &Mode{
			Contains: []*Mode{
				{
					Begin:    `\(`,
					End:      `\)`,
					Keywords: Keywords{"keyword": strings.Join(swiftAvailabilityKeywords, " ")},
					Contains: append(append([]*Mode{}, operators...), number, stringMode),
				},
			},
		},
	}
	keywordAttribute := &Mode{ClassName: "keyword", Match: `@` + eitherJoin(swiftKeywordAttributes)}
	userDefinedAttribute := &Mode{ClassName: "meta", Match: `@` + swiftIdentifier}
	attributes := []*Mode{availableAttribute, keywordAttribute, userDefinedAttribute}

	// The reference's TYPE begins with the zero-width lookahead `\b[A-Z]`. A
	// sliced input makes Go evaluate `\b` at the slice start as a boundary, so
	// check the real preceding byte here instead.
	typeMode := &Mode{Begin: `[A-Z]`, ReturnBegin: true}
	typeMode.beforeBegin = func(m *match, resp *Response) {
		if m.index > 0 && isWordByte(m.input[m.index-1]) {
			resp.IgnoreMatch()
		}
	}
	typeMode.HasRelevance = true
	typeMode.Relevance = 0
	typeMode.Contains = []*Mode{
		Zero(Mode{ClassName: "type", Match: `(AV|CA|CF|CG|CI|CL|CM|CN|CT|MK|MP|MTK|MTL|NS|SCN|SK|UI|WK|XC)` + swiftIdentifierChar + `+`}),
		Zero(Mode{ClassName: "type", Match: swiftTypeIdentifier}),
		Zero(Mode{Match: `[?!]+`}),
		Zero(Mode{Match: `\.\.\.`}),
		Zero(Mode{Match: `\s+&\s+` + swiftTypeIdentifier}),
	}

	genericArguments := &Mode{
		Begin:    `<`,
		End:      `>`,
		Keywords: keywords,
		Contains: append(append(append(append([]*Mode{}, comments...), keywordModes...), attributes...), operatorGuard, typeMode),
	}
	typeMode.Contains = append(typeMode.Contains, genericArguments)

	tupleElementName := Zero(Mode{Match: swiftIdentifier + `\s*:`, Keywords: Keywords{"keyword": "_|0"}})
	tuple := &Mode{
		Begin:    `\(`,
		End:      `\)`,
		Keywords: keywords,
	}
	tuple.HasRelevance = true
	tuple.Relevance = 0
	tuple.Contains = append([]*Mode{Self, tupleElementName}, comments...)
	tuple.Contains = append(tuple.Contains, keywordModes...)
	tuple.Contains = append(tuple.Contains, builtIns...)
	tuple.Contains = append(tuple.Contains, operators...)
	tuple.Contains = append(tuple.Contains, number, stringMode)
	tuple.Contains = append(tuple.Contains, identifiers...)
	tuple.Contains = append(tuple.Contains, attributes...)
	tuple.Contains = append(tuple.Contains, typeMode)

	funcPlusTitle := &Mode{
		BeginKeywords: "func",
		Contains: []*Mode{
			Zero(Mode{ClassName: "title", Match: eitherJoin([]string{"`" + swiftIdentifier + "`", swiftIdentifier, swiftOperator}), EndsParent: true}),
			whitespace,
		},
	}
	genericParameters := &Mode{Begin: `<`, End: `>`, Contains: append(append([]*Mode{}, comments...), typeMode)}
	functionParameterName := &Mode{
		Begin:        eitherJoin([]string{swiftIdentifier + `\s*:`, swiftIdentifier + `\s+` + swiftIdentifier + `\s*:`}),
		ReturnBegin:  true,
		End:          `:`,
		HasRelevance: true,
		Relevance:    0,
		Contains: []*Mode{
			{ClassName: "keyword", Match: `\b_\b`},
			{ClassName: "params", Match: swiftIdentifier},
		},
	}
	functionParametersContains := append([]*Mode{functionParameterName}, comments...)
	functionParametersContains = append(functionParametersContains, keywordModes...)
	functionParametersContains = append(functionParametersContains, operators...)
	functionParametersContains = append(functionParametersContains, number, stringMode)
	functionParametersContains = append(functionParametersContains, attributes...)
	functionParametersContains = append(functionParametersContains, typeMode, tuple)
	functionParameters := &Mode{
		Begin:      `\(`,
		End:        `\)`,
		Keywords:   keywords,
		EndsParent: true,
		Illegal:    `["']`,
		Contains:   functionParametersContains,
	}
	functionMode := &Mode{
		ClassName:   "function",
		Begin:       `\bfunc\b`,
		ReturnBegin: true,
		Contains:    []*Mode{funcPlusTitle, genericParameters, functionParameters, whitespace},
		Illegal:     `\[|%`,
	}
	initSubscript := &Mode{
		ClassName: "function",
		Begin:     `\b(subscript|init[?!]?)\s*`,
		Keywords:  Keywords{"keyword": "subscript init init? init!", "$pattern": `\w+[?!]?`},
		Contains:  []*Mode{genericParameters, functionParameters, whitespace},
		Illegal:   `\[|%`,
	}
	initSubscript.beforeBegin = func(m *match, resp *Response) {
		next := m.index + len(m.lexeme)
		if next >= len(m.input) || (m.input[next] != '<' && m.input[next] != '(') {
			resp.IgnoreMatch()
		}
	}
	operatorDeclaration := &Mode{
		BeginKeywords: "operator",
		End:           MatchNothingRe,
		Contains: []*Mode{
			Zero(Mode{ClassName: "title", Match: swiftOperator, EndsParent: true}),
		},
	}
	precedencegroup := &Mode{
		BeginKeywords: "precedencegroup",
		End:           MatchNothingRe,
		Contains: []*Mode{
			Zero(Mode{ClassName: "title", Match: swiftTypeIdentifier}),
			{
				Begin:        `\{`,
				End:          `\}`,
				HasRelevance: true,
				Relevance:    0,
				EndsParent:   true,
				Keywords:     Keywords{"keyword": strings.Join(append(append([]string{}, swiftPrecedencegroupKeywords...), swiftLiterals...), " ")},
				Contains:     []*Mode{typeMode},
			},
		},
	}
	classMode := &Mode{
		ClassName:     "class",
		BeginKeywords: "struct protocol class extension enum",
		End:           `\{`,
		ExcludeEnd:    true,
		Keywords:      keywords,
		Contains: []*Mode{
			Variant(TitleMode, func(m *Mode) { m.Begin = `[A-Za-z$_][\u00C0-\u02B80-9A-Za-z$_]*` }),
		},
	}
	classMode.Contains = append(classMode.Contains, keywordModes...)
	importMode := Zero(Mode{BeginKeywords: "import", End: `$`, Contains: comments})

	contains := append(append([]*Mode{}, comments...), functionMode, initSubscript, classMode, operatorDeclaration, precedencegroup, importMode)
	contains = append(contains, keywordModes...)
	contains = append(contains, builtIns...)
	contains = append(contains, operators...)
	contains = append(contains, number, stringMode)
	contains = append(contains, identifiers...)
	contains = append(contains, attributes...)
	contains = append(contains, typeMode, tuple)

	return &Language{
		Name:     "Swift",
		Keywords: keywords, Contains: contains,
	}
}

var swiftDotKeywordGuard = regexp.MustCompile(`\A(?:\bProtocol\b|\bType\b|\binit\b|\bself\b)`)
