package markdown

import "regexp"

// pi v1.4.0 packages/tui/src/components/markdown.ts inherits these rules from
// Marked 18.0.11 src/rules.ts. Spell out JavaScript's whitespace and word
// classes: Go's \s is ASCII-only and Go's \w must stay ASCII here.
const inlineSpaceChars = `\t\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`
const inlineSpace = `[` + inlineSpaceChars + `]`
const inlineWordChars = `a-zA-Z0-9_`
const inlineAttribute = inlineSpace + `+[a-zA-Z:_][` + inlineWordChars + `.:-]*(?:` +
	inlineSpace + `*=` + inlineSpace + `*"[^"]*"|` +
	inlineSpace + `*=` + inlineSpace + `*'[^']*'|` +
	inlineSpace + `*=` + inlineSpace + `*[^` + inlineSpaceChars + "\"'=<>`" + `]+)?`

const inlineEmailLabel = `[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?`

// The reference email's final (?![-_]) is redundant here: the next character
// must be the closing '>'. Keep the scheme alternative first, even with '@'.
// Marked's edit() strips an unescaped '^' from the source email class, so its
// compiled rule excludes that character despite its spelling in rules.ts.
var angleAutolinkPattern = regexp.MustCompile(`^<(` +
	`[a-zA-Z][a-zA-Z0-9+.-]{1,31}:[^` + inlineSpaceChars + `\x00-\x1f<>]*` +
	`|[a-zA-Z0-9.!#$%&'*+/=?_` + "`" + `{|}~-]+(@)` + inlineEmailLabel + `(?:\.` + inlineEmailLabel + `)+)>`)

var inlineHTMLPattern = regexp.MustCompile(`(?s)^(?:` +
	`<!--(?:-?>|.*?-->)` +
	`|</[a-zA-Z][` + inlineWordChars + `:-]*` + inlineSpace + `*>` +
	`|<[a-zA-Z][` + inlineWordChars + `-]*(?:` + inlineAttribute + `)*?` + inlineSpace + `*/?>` +
	`|<\?.*?\?>` +
	`|<![a-zA-Z]+` + inlineSpace + `.*?>` +
	`|<!\[CDATA\[.*?\]\]>)`)
