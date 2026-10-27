package highlight

import "strings"

func init() { RegisterLanguage("taggerscript", TaggerScript()) }

// TaggerScript returns the Tagger Script language definition (a port of
// languages/taggerscript.js). The function rule's `(?!noop)` lookahead becomes
// a guard so `$noop(` is left to the comment rule.
func TaggerScript() *Language {
	comment := &Mode{
		ClassName:    "comment",
		Begin:        `\$noop\(`,
		End:          `\)`,
		HasRelevance: true,
		Relevance:    10,
		Contains: []*Mode{
			{
				Begin:    `\(`,
				End:      `\)`,
				Contains: []*Mode{Self, {Begin: `\\.`}},
			},
		},
	}

	function := &Mode{
		ClassName:  "keyword",
		Begin:      `\$[a-zA-Z][_a-zA-Z0-9]*`,
		End:        `\(`,
		ExcludeEnd: true,
	}
	function.beforeBegin = func(m *match, resp *Response) {
		if strings.HasPrefix(m.input[m.index+1:], "noop") {
			resp.IgnoreMatch()
		}
	}

	return &Language{
		Name: "Tagger Script",
		Contains: []*Mode{
			comment,
			function,
			{ClassName: "variable", Begin: `%[_a-zA-Z0-9:]*`, End: `%`},
			{ClassName: "symbol", Begin: `\\.`},
		},
	}
}
