package highlight

func init() { RegisterLanguage("erlang-repl", ErlangRepl()) }

// ErlangRepl returns the Erlang REPL language definition (a port of
// languages/erlang-repl.js).
func ErlangRepl() *Language {
	return &Language{
		Name: "Erlang REPL",
		Keywords: Keywords{
			"built_in": "spawn spawn_link self",
			"keyword": "after and andalso|10 band begin bnot bor bsl bsr bxor case catch cond div end fun if let not of or " +
				"orelse|10 query receive rem try when xor",
		},
		Contains: []*Mode{
			{ClassName: "meta", Begin: `^[0-9]+> `, HasRelevance: true, Relevance: 10},
			CommentMode(`%`, `$`),
			{
				ClassName:    "number",
				Begin:        `\b(\d+(_\d+)*#[a-fA-F0-9]+(_[a-fA-F0-9]+)*|\d+(_\d+)*(\.\d+(_\d+)*)?([eE][-+]?\d+)?)`,
				HasRelevance: true,
				Relevance:    0,
			},
			AposStringMode,
			QuoteStringMode,
			{Begin: `\?(::)?([A-Z]\w*)((::)[A-Z]\w*)*`},
			{Begin: `->`},
			{Begin: `ok`},
			{Begin: `!`},
			{
				Begin:        `(\b[a-z'][a-zA-Z0-9_']*:[a-z'][a-zA-Z0-9_']*)|(\b[a-z'][a-zA-Z0-9_']*)`,
				HasRelevance: true,
				Relevance:    0,
			},
			{Begin: `[A-Z][a-zA-Z0-9_']*`, HasRelevance: true, Relevance: 0},
		},
	}
}
