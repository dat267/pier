package highlight

func init() { RegisterLanguage("abnf", ABNF()) }

// abnfRuleGuard is the reference's `(?=\s*=)` lookahead for a rule declaration.
var abnfRuleGuard = mustCompileJS(`\s*=`)

// ABNF returns the Augmented Backus-Naur Form language definition (a port of
// languages/abnf.js).
func ABNF() *Language {
	ruleDeclaration := &Mode{ClassName: "attribute", Begin: `^[a-zA-Z][a-zA-Z0-9-]*`}
	ruleDeclaration.beforeBegin = func(m *match, resp *Response) {
		if !startsWith(abnfRuleGuard, m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}
	return &Language{
		Name:    "Augmented Backus-Naur Form",
		Illegal: "[!@#$^&',?+~`|:]",
		Keywords: Keywords{
			"keyword": "ALPHA BIT CHAR CR CRLF CTL DIGIT DQUOTE HEXDIG HTAB LF LWSP OCTET SP VCHAR WSP",
		},
		Contains: []*Mode{
			ruleDeclaration,
			CommentMode(`;`, `$`),
			{ClassName: "symbol", Begin: `%b[0-1]+(-[0-1]+|(\.[0-1]+)+){0,1}`},
			{ClassName: "symbol", Begin: `%d[0-9]+(-[0-9]+|(\.[0-9]+)+){0,1}`},
			{ClassName: "symbol", Begin: `%x[0-9A-F]+(-[0-9A-F]+|(\.[0-9A-F]+)+){0,1}`},
			{ClassName: "symbol", Begin: `%[si]`},
			QuoteStringMode,
			NumberMode,
		},
	}
}
