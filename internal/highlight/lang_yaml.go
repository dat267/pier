package highlight

import "strings"

func init() { RegisterLanguage("yaml", YAML()) }

const (
	yamlLiterals      = "true false yes no null"
	yamlURICharacters = `[\w#;/?:@&=+$,.~*'()[\]]+`
)

// yamlAfterColon is the reference's `(?=[ \t]|$)` after a key's colon.
func yamlAfterColon(m *match) bool {
	end := m.index + len(m.lexeme)
	if end >= len(m.input) {
		return true
	}
	c := m.input[end]
	return c == ' ' || c == '\t' || c == '\n'
}

// yamlAfterDash is the reference's `(?=[ ]|$)` after a block-sequence bullet.
func yamlAfterDash(m *match) bool {
	end := m.index + len(m.lexeme)
	if end >= len(m.input) {
		return true
	}
	return m.input[end] == ' ' || m.input[end] == '\n'
}

// yamlWordFirsts is the first-byte set of `\w`: the unquoted key's manual
// matcher can start at any word byte.
const yamlWordFirsts = "_0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// yamlKeyEnd matches `\w[\w :\/.-]*:(?=[ \t]|$)`. The greedy class includes
// the colon, and the reference's lookahead makes JS backtrack to the last colon
// followed by whitespace or end of line; a post-hoc guard cannot, so this is a
// manual matcher.
func yamlKeyEnd(s string, pos int) (int, bool) {
	if pos >= len(s) || !isWordByte(s[pos]) {
		return 0, false
	}
	end := pos + 1
	for end < len(s) && yamlKeyClassByte(s[end]) {
		end++
	}
	for i := end - 1; i >= pos; i-- {
		if s[i] != ':' {
			continue
		}
		if i+1 >= len(s) || s[i+1] == ' ' || s[i+1] == '\t' || s[i+1] == '\n' {
			return i + 1, true
		}
	}
	return 0, false
}

func yamlKeyClassByte(b byte) bool {
	return isWordByte(b) || b == ' ' || b == ':' || b == '/' || b == '.' || b == '-'
}

// yamlBlockScalarEnd matches the reference's multi-line string begin,
//
//	[\|>]([1-9]?[+-])?[ ]*\n( +)[^ ][^\n]*\n(\2[^\n]+\n?)*
//
// whose trailing group is an indentation backreference RE2 cannot express. It
// returns the end offset of the whole block.
func yamlBlockScalarEnd(s string, pos int) (int, bool) {
	i := pos
	if i >= len(s) || (s[i] != '|' && s[i] != '>') {
		return 0, false
	}
	i++
	// ([1-9]?[+-])?: an optional digit followed by a required + or -.
	if i+1 < len(s) && s[i] >= '1' && s[i] <= '9' && (s[i+1] == '+' || s[i+1] == '-') {
		i += 2
	} else if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < len(s) && s[i] == ' ' {
		i++
	}
	if i >= len(s) || s[i] != '\n' {
		return 0, false
	}
	i++
	indentStart := i
	for i < len(s) && s[i] == ' ' {
		i++
	}
	if i == indentStart || i >= len(s) || s[i] == ' ' || s[i] == '\n' {
		return 0, false
	}
	indent := s[indentStart:i]
	for i < len(s) && s[i] != '\n' {
		i++
	}
	if i >= len(s) || s[i] != '\n' {
		return 0, false
	}
	i++
	for strings.HasPrefix(s[i:], indent) {
		j := i + len(indent)
		if j >= len(s) || s[j] == '\n' {
			break
		}
		for j < len(s) && s[j] != '\n' {
			j++
		}
		if j < len(s) && s[j] == '\n' {
			j++
		}
		i = j
	}
	return i, true
}

// YAML returns the YAML language definition (a port of languages/yaml.js).
func YAML() *Language {
	keyBase := &Mode{ClassName: "attr"}
	keyBase.Variants = []*Mode{
		{ClassName: "attr", ManualBegin: yamlKeyEnd, ManualFirst: yamlWordFirsts},
		Variant(keyBase, func(m *Mode) { m.Begin = `"\w[\w :\/.-]*":` }),
		Variant(keyBase, func(m *Mode) { m.Begin = `'\w[\w :\/.-]*':` }),
	}
	for _, v := range keyBase.Variants[1:] {
		v.beforeBegin = func(m *match, resp *Response) {
			if !yamlAfterColon(m) {
				resp.IgnoreMatch()
			}
		}
	}

	templateBase := &Mode{ClassName: "template-variable"}
	templateBase.Variants = []*Mode{
		Variant(templateBase, func(m *Mode) { m.Begin = `\{\{`; m.End = `\}\}` }),
		Variant(templateBase, func(m *Mode) { m.Begin = `%\{`; m.End = `\}` }),
	}

	stringBase := &Mode{ClassName: "string", HasRelevance: true, Relevance: 0, Contains: []*Mode{BackslashEscape, templateBase}}
	stringBase.Variants = []*Mode{
		Variant(stringBase, func(m *Mode) { m.Begin = `'`; m.End = `'` }),
		Variant(stringBase, func(m *Mode) { m.Begin = `"`; m.End = `"` }),
		Variant(stringBase, func(m *Mode) { m.Begin = `\S+` }),
	}

	containerString := &Mode{ClassName: "string", HasRelevance: true, Relevance: 0, Contains: []*Mode{BackslashEscape, templateBase}}
	containerString.Variants = []*Mode{
		Variant(containerString, func(m *Mode) { m.Begin = `'`; m.End = `'` }),
		Variant(containerString, func(m *Mode) { m.Begin = `"`; m.End = `"` }),
		Variant(containerString, func(m *Mode) { m.Begin = `[^\s,{}[\]]+` }),
	}

	const dateRe = `[0-9]{4}(-[0-9][0-9]){0,2}`
	const timeRe = `([Tt \t][0-9][0-9]?(:[0-9][0-9]){2})?`
	const fractionRe = `(\.[0-9]*)?`
	const zoneRe = `([ \t])*(Z|[-+][0-9][0-9]?(:[0-9][0-9])?)?`
	timestamp := &Mode{ClassName: "number", Begin: `\b` + dateRe + timeRe + fractionRe + zoneRe + `\b`}

	valueContainer := &Mode{
		End:            `,`,
		EndsWithParent: true,
		ExcludeEnd:     true,
		Keywords:       Keywords{"literal": yamlLiterals},
		HasRelevance:   true,
		Relevance:      0,
	}
	object := &Mode{
		Begin:        `\{`,
		End:          `\}`,
		Contains:     []*Mode{valueContainer},
		Illegal:      `\n`,
		HasRelevance: true,
		Relevance:    0,
	}
	array := &Mode{
		Begin:        `\[`,
		End:          `\]`,
		Contains:     []*Mode{valueContainer},
		Illegal:      `\n`,
		HasRelevance: true,
		Relevance:    0,
	}

	bullet := &Mode{ClassName: "bullet", Begin: `-`, HasRelevance: true, Relevance: 0}
	bullet.beforeBegin = func(m *match, resp *Response) {
		if !yamlAfterDash(m) {
			resp.IgnoreMatch()
		}
	}

	blockScalar := &Mode{ClassName: "string", ManualBegin: yamlBlockScalarEnd, ManualFirst: "|>"}

	modes := []*Mode{
		keyBase,
		{ClassName: "meta", Begin: `^---\s*$`, HasRelevance: true, Relevance: 10},
		blockScalar,
		{
			Begin:        `<%[%=-]?`,
			End:          `[%-]?%>`,
			SubLanguage:  []string{"ruby"},
			ExcludeBegin: true,
			ExcludeEnd:   true,
			HasRelevance: true,
			Relevance:    0,
		},
		{ClassName: "type", Begin: `!\w+!` + yamlURICharacters},
		{ClassName: "type", Begin: `!<` + yamlURICharacters + `>`},
		{ClassName: "type", Begin: `!` + yamlURICharacters},
		{ClassName: "type", Begin: `!!` + yamlURICharacters},
		{ClassName: "meta", Begin: `&` + UnderscoreIdentRe + `$`},
		{ClassName: "meta", Begin: `\*` + UnderscoreIdentRe + `$`},
		bullet,
		HashCommentMode,
		{BeginKeywords: yamlLiterals, Keywords: Keywords{"literal": yamlLiterals}},
		timestamp,
		{ClassName: "number", Begin: CNumberRe + `\b`, HasRelevance: true, Relevance: 0},
		object,
		array,
		stringBase,
	}

	valueModes := append([]*Mode{}, modes[:len(modes)-1]...)
	valueModes = append(valueModes, containerString)
	valueContainer.Contains = valueModes

	return &Language{
		Name:            "YAML",
		Aliases:         []string{"yml"},
		CaseInsensitive: true,
		Mode: Mode{
			Contains: modes,
		},
	}
}
