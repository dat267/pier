package highlight

import (
	"strings"
)

func init() { RegisterLanguage("ruby", Ruby()) }

func rubyDoctag() *Mode { return Zero(Mode{ClassName: "doctag", Begin: `@[A-Za-z]+`}) }

// rubyHeredocBegin implements the reference's `<<[-~]?'?(\w+)\n(?:[^\n]*\n)*?\s*\1\b`
// backreference rule: the closing name must equal the opening name.
func rubyHeredocBegin(s string, pos int) (int, bool) {
	i := pos
	if i+1 >= len(s) || s[i] != '<' || s[i+1] != '<' {
		return 0, false
	}
	i += 2
	if i < len(s) && (s[i] == '-' || s[i] == '~') {
		i++
	}
	if i < len(s) && s[i] == '\'' {
		i++
	}
	start := i
	for i < len(s) && isWordByte(s[i]) {
		i++
	}
	if i == start {
		return 0, false
	}
	name := s[start:i]
	if i >= len(s) || s[i] != '\n' {
		return 0, false
	}
	i++
	for i < len(s) {
		lineStart := i
		var line string
		if nl := strings.IndexByte(s[i:], '\n'); nl < 0 {
			line = s[i:]
			i = len(s)
		} else {
			line = s[i : i+nl]
			i += nl + 1
		}
		j := 0
		for j < len(line) && isWhitespaceByte(line[j]) {
			j++
		}
		rest := line[j:]
		if strings.HasPrefix(rest, name) && (len(rest) == len(name) || !isWordByte(rest[len(name)])) {
			return lineStart + j + len(name), true
		}
	}
	return 0, false
}

// Ruby returns the Ruby language definition (a port of languages/ruby.js).
func Ruby() *Language {
	const rubyMethodRe = `([a-zA-Z_]\w*[!?=]?|[-+~]@|<<|>>|=~|===?|<=>|[<>]=?|\*\*|[-/+%^&*~\x60|]|\[\]=?)`

	rubyKeywords := Keywords{
		"keyword": "and then defined module in return redo if BEGIN retry end for self when " +
			"next until do begin unless END rescue else break undef not super class case " +
			"require yield alias while ensure elsif or include attr_reader attr_writer attr_accessor " +
			"__FILE__",
		"built_in": "proc lambda",
		"literal":  "true false nil",
	}

	irbObject := &Mode{Begin: `#<`, End: `>`}

	hashComment := commentMode(`#`, `$`)
	hashComment.Contains = []*Mode{rubyDoctag(), PhrasalWordsMode, Zero(Mode{ClassName: "doctag", Begin: `(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):`})}
	beginComment := commentMode(`^=begin`, `^=end`)
	beginComment.HasRelevance = true
	beginComment.Relevance = 10
	beginComment.Contains = []*Mode{rubyDoctag(), PhrasalWordsMode, Zero(Mode{ClassName: "doctag", Begin: `(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):`})}
	endComment := commentMode(`^__END__`, `\n$`)

	subst := &Mode{ClassName: "subst", Begin: `#\{`, End: `\}`, Keywords: rubyKeywords}

	heredoc := &Mode{
		ClassName:   "string",
		ReturnBegin: true,
		ManualBegin: rubyHeredocBegin,
		ManualFirst: "<",
		Contains: []*Mode{
			{Begin: `<<[-~]?'?`},
			EndSameAsBegin(&Mode{Begin: `(\w+)`, End: `(\w+)`, Contains: []*Mode{BackslashEscape, subst}}),
		},
	}
	stringMode := &Mode{
		ClassName: "string",
		Contains:  []*Mode{BackslashEscape, subst},
		Variants: []*Mode{
			{ClassName: "string", Begin: `'`, End: `'`, Contains: []*Mode{BackslashEscape, subst}},
			{ClassName: "string", Begin: `"`, End: `"`, Contains: []*Mode{BackslashEscape, subst}},
			{ClassName: "string", Begin: "`", End: "`", Contains: []*Mode{BackslashEscape, subst}},
			{ClassName: "string", Begin: `%[qQwWx]?\(`, End: `\)`, Contains: []*Mode{BackslashEscape, subst}},
			{ClassName: "string", Begin: `%[qQwWx]?\[`, End: `\]`, Contains: []*Mode{BackslashEscape, subst}},
			{ClassName: "string", Begin: `%[qQwWx]?\{`, End: `\}`, Contains: []*Mode{BackslashEscape, subst}},
			{ClassName: "string", Begin: `%[qQwWx]?<`, End: `>`, Contains: []*Mode{BackslashEscape, subst}},
			{ClassName: "string", Begin: `%[qQwWx]?/`, End: `/`, Contains: []*Mode{BackslashEscape, subst}},
			{ClassName: "string", Begin: `%[qQwWx]?%`, End: `%`, Contains: []*Mode{BackslashEscape, subst}},
			{ClassName: "string", Begin: `%[qQwWx]?-`, End: `-`, Contains: []*Mode{BackslashEscape, subst}},
			{ClassName: "string", Begin: `%[qQwWx]?\|`, End: `\|`, Contains: []*Mode{BackslashEscape, subst}},
			{ClassName: "string", Begin: `\B\?(\\\d{1,3})`},
			{ClassName: "string", Begin: `\B\?(\\x[A-Fa-f0-9]{1,2})`},
			{ClassName: "string", Begin: `\B\?(\\u\{?[A-Fa-f0-9]{1,6}\}?)`},
			{ClassName: "string", Begin: `\B\?(\\M-\\C-|\\M-\\c|\\c\\M-|\\M-|\\C-\\M-)[\x20-\x7e]`},
			{ClassName: "string", Begin: `\B\?\\(c|C-)[\x20-\x7e]`},
			{ClassName: "string", Begin: `\B\?\\?\S`},
			heredoc,
		},
	}

	const decimal = `[1-9](_?[0-9])*|0`
	const digits = `[0-9](_?[0-9])*`
	number := Zero(Mode{
		ClassName: "number",
		Variants: []*Mode{
			{ClassName: "number", Begin: `\b(` + decimal + `)(\.(` + digits + `))?([eE][+-]?(` + digits + `)|r)?i?\b`},
			{ClassName: "number", Begin: `\b0[dD][0-9](_?[0-9])*r?i?\b`},
			{ClassName: "number", Begin: `\b0[bB][0-1](_?[0-1])*r?i?\b`},
			{ClassName: "number", Begin: `\b0[oO][0-7](_?[0-7])*r?i?\b`},
			{ClassName: "number", Begin: `\b0[xX][0-9a-fA-F](_?[0-9a-fA-F])*r?i?\b`},
			{ClassName: "number", Begin: `\b0(_?[0-7])+r?i?\b`},
		},
	})

	params := &Mode{ClassName: "params", Begin: `\(`, End: `\)`, EndsParent: true, Keywords: rubyKeywords}

	titleClass := Variant(TitleMode, func(m *Mode) { m.Begin = `[A-Za-z_]\w*(::\w+)*(\?|!)?` })
	classMode := &Mode{
		ClassName:     "class",
		BeginKeywords: "class module",
		End:           `$|;`,
		Illegal:       `=`,
		Contains: []*Mode{
			titleClass,
			{
				Begin: `<\\s*`,
				Contains: []*Mode{
					Zero(Mode{Begin: `(` + IdentRe + `::)?` + IdentRe}),
				},
			},
		},
	}
	functionMode := Zero(Mode{
		ClassName: "function",
		Begin:     `def\s+`,
		Keywords:  Keywords{"keyword": "def"},
		End:       `$|;`,
		Contains: []*Mode{
			Variant(TitleMode, func(m *Mode) { m.Begin = rubyMethodRe }),
			params,
		},
	})
	// The reference's `def\s+(?=RUBY_METHOD_RE\s*(\(|;|$))` lookahead.
	defGuard := mustCompileJS(`\A` + rubyMethodRe + `\s*(\(|;|$)`)
	functionMode.beforeBegin = func(m *match, resp *Response) {
		if !defGuard.MatchString(m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}

	symbolColon := &Mode{
		ClassName: "symbol",
		Begin:     `:`,
		Contains: []*Mode{
			stringMode,
			{Begin: rubyMethodRe},
		},
	}
	symbolColon.beforeBegin = func(m *match, resp *Response) {
		next := m.index + len(m.lexeme)
		if next < len(m.input) && isWhitespaceByte(m.input[next]) {
			resp.IgnoreMatch()
		}
	}
	variable := &Mode{
		ClassName: "variable",
		Begin:     `(\$\W)|((\$|@@?)(\w+))`,
	}
	variable.beforeBegin = func(m *match, resp *Response) {
		next := m.index + len(m.lexeme)
		if next >= len(m.input) {
			resp.IgnoreMatch()
			return
		}
		b := m.input[next]
		if b == '@' || b == '$' || b == '?' || b == '\'' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') {
			resp.IgnoreMatch()
		}
	}
	pipeParams := &Mode{ClassName: "params", Begin: `\|`, End: `\|`, HasRelevance: true, Relevance: 0, Keywords: rubyKeywords}
	regexpBase := &Mode{
		ClassName: "regexp",
		Illegal:   `\n`,
		Contains:  []*Mode{BackslashEscape, subst},
	}
	regexpBase.Variants = []*Mode{
		Variant(regexpBase, func(m *Mode) { m.Begin = `/`; m.End = `/[a-z]*` }),
		Variant(regexpBase, func(m *Mode) { m.Begin = `%r\{`; m.End = `\}[a-z]*` }),
		Variant(regexpBase, func(m *Mode) { m.Begin = `%r\(`; m.End = `\)[a-z]*` }),
		Variant(regexpBase, func(m *Mode) { m.Begin = `%r!`; m.End = `![a-z]*` }),
		Variant(regexpBase, func(m *Mode) { m.Begin = `%r\[`; m.End = `\][a-z]*` }),
	}
	regexpContainer := Zero(Mode{
		Begin:    `(` + reStartersRe + `|unless)\s*`,
		Keywords: Keywords{"keyword": "unless"},
		Contains: []*Mode{regexpBase},
	})

	defaultContains := []*Mode{
		stringMode,
		classMode,
		functionMode,
		{Begin: IdentRe + `::`},
		Zero(Mode{ClassName: "symbol", Begin: UnderscoreIdentRe + `(!|\?)?:`}),
		symbolColon,
		number,
		variable,
		pipeParams,
		regexpContainer,
	}
	subst.Contains = defaultContains
	params.Contains = defaultContains

	classMode.Contains = append(classMode.Contains, irbObject, hashComment, beginComment, endComment)
	functionMode.Contains = append(functionMode.Contains, irbObject, hashComment, beginComment, endComment)
	regexpContainer.Contains = append(regexpContainer.Contains, irbObject, hashComment, beginComment, endComment)
	commentModes := []*Mode{irbObject, hashComment, beginComment, endComment}

	irbDefault := []*Mode{
		{
			Begin:  `^\s*=>`,
			Starts: &Mode{End: `$`, Contains: defaultContains},
		},
		{
			ClassName: "meta",
			Begin:     `^([>?]>|[\w#]+\(\w+\):\d+:\d+>|(\w+-)?\d+\.\d+\.\d+(p\d+)?[^\d][^>]+>)`,
			Starts:    &Mode{End: `$`, Contains: defaultContains},
		},
	}
	irbMeta := irbDefault[1]
	irbMeta.beforeBegin = func(m *match, resp *Response) {
		next := m.index + len(m.lexeme)
		if next >= len(m.input) || m.input[next] != ' ' {
			resp.IgnoreMatch()
		}
	}

	contains := []*Mode{Shebang("ruby", 0)}
	contains = append(contains, irbDefault...)
	contains = append(contains, commentModes...)
	contains = append(contains, defaultContains...)

	return &Language{
		Name:     "Ruby",
		Aliases:  []string{"rb", "gemspec", "podspec", "thor", "irb"},
		Keywords: rubyKeywords,
		Illegal:  `\/\*`,
		Contains: contains,
	}
}
