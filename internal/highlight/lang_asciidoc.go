package highlight

// asciidocDelimitedEnd matches a multi-line delimited span whose reference begin
// is `DD((A(?!D)|\\[^\n]|[^D\n\\])+\n)+(A(?!D)|\\[^\n]|[^D\n\\])*DD`:
// one or more non-empty lines ending in a newline, then a final partial line,
// then the closing `DD`. The `(?!D)` atoms mean a lone delimiter byte is body
// text while `DD` closes, and the line structure rejects a blank line. The
// embedded negative lookaheads have no RE2 equivalent.
func asciidocDelimitedEnd(s string, pos int, d byte) (int, bool) {
	if pos+1 >= len(s) || s[pos] != d || s[pos+1] != d {
		return 0, false
	}
	i := pos + 2
	lines := 0
	for {
		lineStart := i
		for i < len(s) && s[i] != '\n' {
			if s[i] == d && i+1 < len(s) && s[i+1] == d {
				break
			}
			if s[i] == '\\' {
				if i+1 >= len(s) || s[i+1] == '\n' {
					return 0, false
				}
				i += 2
				continue
			}
			i++
		}
		if i < len(s) && s[i] == d && i+1 < len(s) && s[i+1] == d {
			if lines == 0 {
				return 0, false
			}
			return i + 2, true
		}
		if i >= len(s) || s[i] != '\n' || i == lineStart {
			return 0, false
		}
		lines++
		i++
	}
}

var (
	asciidocM0  *Mode
	asciidocM1  *Mode
	asciidocM2  *Mode
	asciidocM3  *Mode
	asciidocM4  *Mode
	asciidocM5  *Mode
	asciidocM6  *Mode
	asciidocM7  *Mode
	asciidocM8  *Mode
	asciidocM9  *Mode
	asciidocM10 *Mode
	asciidocM11 *Mode
	asciidocM12 *Mode
	asciidocM13 *Mode
	asciidocM14 *Mode
	asciidocM15 *Mode
	asciidocM16 *Mode
	asciidocM17 *Mode
	asciidocM18 *Mode
	asciidocM19 *Mode
	asciidocM20 *Mode
	asciidocM21 *Mode
	asciidocM22 *Mode
	asciidocM23 *Mode
	asciidocM24 *Mode
	asciidocM25 *Mode
	asciidocM26 *Mode
	asciidocM27 *Mode
	asciidocM28 *Mode
	asciidocM29 *Mode
	asciidocM30 *Mode
	asciidocM31 *Mode
	asciidocM32 *Mode
	asciidocM33 *Mode
	asciidocM34 *Mode
	asciidocM35 *Mode
	asciidocM36 *Mode
	asciidocM37 *Mode
	asciidocM38 *Mode
	asciidocM39 *Mode
	asciidocM40 *Mode
	asciidocM41 *Mode
	asciidocM42 *Mode
	asciidocM43 *Mode
)

func init() {
	asciidocM0 = &Mode{ClassName: "comment", Begin: "^/{4,}\\n", End: "\\n/{4,}$", HasRelevance: true, Relevance: 10}
	asciidocM1 = &Mode{Begin: "\\b(a|an|the|are|I'm|isn't|don't|doesn't|won't|but|just|should|pretty|simply|enough|gonna|going|wtf|so|such|will|you|your|they|like|more)\\b"}
	asciidocM2 = &Mode{ClassName: "doctag", Begin: "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):", HasRelevance: true, Relevance: 0}
	asciidocM3 = &Mode{ClassName: "comment", Begin: "^//", End: "$", HasRelevance: true, Relevance: 0}
	asciidocM4 = &Mode{ClassName: "doctag", Begin: "(?:TODO|FIXME|NOTE|BUG|OPTIMIZE|HACK|XXX):", HasRelevance: true, Relevance: 0}
	asciidocM5 = &Mode{ClassName: "title", Begin: "^\\.\\w.*$"}
	asciidocM6 = &Mode{Begin: "^[=\\*]{4,}\\n", End: "\\n^[=\\*]{4,}$", HasRelevance: true, Relevance: 10}
	asciidocM7 = &Mode{ClassName: "section", HasRelevance: true, Relevance: 10}
	asciidocM8 = &Mode{ClassName: "section", Begin: "^={1,6}[ 	].+$", HasRelevance: true, Relevance: 10}
	asciidocM9 = &Mode{ClassName: "section", Begin: "^[^\\[\\]\\n]+?\\n[=\\-~\\^\\+]{2,}$", HasRelevance: true, Relevance: 10}
	asciidocM10 = &Mode{ClassName: "meta", Begin: "^:.+?:", End: "\\s", ExcludeEnd: true, HasRelevance: true, Relevance: 10}
	asciidocM11 = &Mode{ClassName: "meta", Begin: "^\\[.+?\\]$", HasRelevance: true, Relevance: 0}
	asciidocM12 = &Mode{ClassName: "quote", Begin: "^_{4,}\\n", End: "\\n_{4,}$", HasRelevance: true, Relevance: 10}
	asciidocM13 = &Mode{ClassName: "code", Begin: "^[\\-\\.]{4,}\\n", End: "\\n[\\-\\.]{4,}$", HasRelevance: true, Relevance: 10}
	asciidocM14 = &Mode{Begin: "^\\+{4,}\\n", End: "\\n\\+{4,}$", HasRelevance: true, Relevance: 10}
	asciidocM15 = &Mode{Begin: "<", End: ">", HasRelevance: true, Relevance: 0, SubLanguage: []string{"xml"}}
	asciidocM16 = &Mode{ClassName: "bullet", Begin: "^(\\*+|-+|\\.+|[^\\n]+?::)\\s+"}
	asciidocM17 = &Mode{ClassName: "symbol", Begin: "^(NOTE|TIP|IMPORTANT|WARNING|CAUTION):\\s+", HasRelevance: true, Relevance: 10}
	asciidocM18 = &Mode{Begin: "\\\\[*_`]"}
	asciidocM19 = &Mode{Begin: "\\\\\\\\\\*{2}[^\\n]*?\\*{2}"}
	asciidocM20 = &Mode{Begin: "\\\\\\\\_{2}[^\\n]*_{2}"}
	asciidocM21 = &Mode{Begin: "\\\\\\\\`{2}[^\\n]*`{2}"}
	asciidocM22 = &Mode{Begin: "[:;}][*_`]", BeginNotFollowedBy: "[*_`]"}
	asciidocM23 = &Mode{ClassName: "strong", Begin: "\\*{2}([^\\n]+?)\\*{2}"}
	asciidocM24 = &Mode{ClassName: "strong", ManualBegin: func(s string, pos int) (int, bool) { return asciidocDelimitedEnd(s, pos, '*') }, ManualFirst: "*", HasRelevance: true, Relevance: 0}
	asciidocM25 = &Mode{ClassName: "strong", Begin: "\\B\\*(\\S|\\S[^\\n]*?\\S)\\*", BeginNotFollowedBy: "\\w"}
	asciidocM26 = &Mode{ClassName: "strong", Begin: "\\*[^\\s]([^\\n]+\\n)+([^\\n]+)\\*"}
	asciidocM27 = &Mode{ClassName: "emphasis", Begin: "_{2}([^\\n]+?)_{2}"}
	asciidocM28 = &Mode{ClassName: "emphasis", ManualBegin: func(s string, pos int) (int, bool) { return asciidocDelimitedEnd(s, pos, '_') }, ManualFirst: "_", HasRelevance: true, Relevance: 0}
	asciidocM29 = &Mode{ClassName: "emphasis", Begin: "\\b_(\\S|\\S[^\\n]*?\\S)_", BeginNotFollowedBy: "\\w"}
	asciidocM30 = &Mode{ClassName: "emphasis", Begin: "_[^\\s]([^\\n]+\\n)+([^\\n]+)_"}
	asciidocM31 = &Mode{ClassName: "emphasis", Begin: "\\B'", BeginNotFollowedBy: "['\\s]", End: "(\\n{2}|')", HasRelevance: true, Relevance: 0}
	asciidocM32 = &Mode{Begin: "\\\\'\\w", HasRelevance: true, Relevance: 0}
	asciidocM33 = &Mode{ClassName: "string"}
	asciidocM34 = &Mode{ClassName: "string", Begin: "``.+?''"}
	asciidocM35 = &Mode{ClassName: "string", Begin: "`.+?'"}
	asciidocM36 = &Mode{ClassName: "code", Begin: "`{2}", End: "(\\n{2}|`{2})"}
	asciidocM37 = &Mode{ClassName: "code", Begin: "(`.+?`|\\+.+?\\+)", HasRelevance: true, Relevance: 0}
	asciidocM38 = &Mode{ClassName: "code", Begin: "^[ \\t]", End: "$", HasRelevance: true, Relevance: 0}
	asciidocM39 = &Mode{Begin: "^'{3,}[ \\t]*$", HasRelevance: true, Relevance: 10}
	asciidocM40 = &Mode{Begin: "(link:)?(http|https|ftp|file|irc|image:?):\\S+?\\[[^[]*?\\]", ReturnBegin: true, HasRelevance: true, Relevance: 10}
	asciidocM41 = &Mode{Begin: "(link|image:?):", HasRelevance: true, Relevance: 0}
	asciidocM42 = &Mode{ClassName: "link", Begin: "\\w", End: "[^\\[]+", HasRelevance: true, Relevance: 0}
	asciidocM43 = &Mode{ClassName: "string", Begin: "\\[", End: "\\]", ExcludeBegin: true, ExcludeEnd: true, HasRelevance: true, Relevance: 0}
	asciidocM0.Contains = []*Mode{asciidocM1, asciidocM2}
	asciidocM3.Contains = []*Mode{asciidocM1, asciidocM4}
	asciidocM7.Variants = []*Mode{asciidocM8, asciidocM9}
	asciidocM14.Contains = []*Mode{asciidocM15}
	asciidocM31.Contains = []*Mode{asciidocM32}
	asciidocM33.Variants = []*Mode{asciidocM34, asciidocM35}
	asciidocM40.Contains = []*Mode{asciidocM41, asciidocM42, asciidocM43}
	RegisterLanguage("asciidoc", &Language{
		Name:            "AsciiDoc",
		Aliases:         []string{"adoc"},
		CaseInsensitive: false,
		Contains:        []*Mode{asciidocM0, asciidocM3, asciidocM5, asciidocM6, asciidocM7, asciidocM10, asciidocM11, asciidocM12, asciidocM13, asciidocM14, asciidocM16, asciidocM17, asciidocM18, asciidocM19, asciidocM20, asciidocM21, asciidocM22, asciidocM23, asciidocM24, asciidocM25, asciidocM26, asciidocM27, asciidocM28, asciidocM29, asciidocM30, asciidocM31, asciidocM33, asciidocM36, asciidocM37, asciidocM38, asciidocM39, asciidocM40},
	})
}
