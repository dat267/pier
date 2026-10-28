package highlight

func init() { RegisterLanguage("http", HTTP()) }

// The response and request rules open with a zero-width lookahead at the line
// start; RE2 has none, so each becomes a beforeBegin guard.
var (
	httpVersionGuard = mustCompileJS(`\AHTTP/(2|1\.[01]) \d{3}`)
	httpRequestGuard = mustCompileJS(`\A[A-Z]+ .* HTTP/(2|1\.[01])$`)
	httpHeaderGuard  = mustCompileJS(`\A:\s`)
)

// HTTP returns the HTTP language definition (a port of languages/http.js).
func HTTP() *Language {
	const version = `HTTP/(2|1\.[01])`
	const headerName = `[A-Za-z][A-Za-z0-9-]*`

	header := &Mode{ClassName: "attribute", Begin: `^` + headerName}
	header.beforeBegin = func(m *match, resp *Response) {
		if !startsWith(httpHeaderGuard, m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}
	header.Starts = &Mode{
		Contains: []*Mode{
			{
				ClassName:    "punctuation",
				Begin:        `: `,
				HasRelevance: true,
				Relevance:    0,
				Starts:       &Mode{End: `$`, HasRelevance: true, Relevance: 0},
			},
		},
	}

	headersAndBody := []*Mode{
		header,
		{
			Begin:  "\n\n",
			Starts: &Mode{SubLanguage: []string{}, EndsWithParent: true},
		},
	}

	responseStarts := &Mode{End: `\b\B`, Illegal: `\S`, Contains: headersAndBody}
	response := &Mode{
		Begin: `^`,
		End:   `$`,
		Contains: []*Mode{
			{ClassName: "meta", Begin: version},
			{ClassName: "number", Begin: `\b\d{3}\b`},
		},
		Starts: responseStarts,
	}
	response.beforeBegin = func(m *match, resp *Response) {
		if !startsWith(httpVersionGuard, m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}

	requestStarts := &Mode{End: `\b\B`, Illegal: `\S`, Contains: headersAndBody}
	request := &Mode{
		Begin: `^`,
		End:   `$`,
		Contains: []*Mode{
			{ClassName: "string", Begin: ` `, End: ` `, ExcludeBegin: true, ExcludeEnd: true},
			{ClassName: "meta", Begin: version},
			{ClassName: "keyword", Begin: `[A-Z]+`},
		},
		Starts: requestStarts,
	}
	request.beforeBegin = func(m *match, resp *Response) {
		if !startsWith(httpRequestGuard, m.input[m.index+len(m.lexeme):]) {
			resp.IgnoreMatch()
		}
	}

	return &Language{
		Name:    "HTTP",
		Aliases: []string{"https"},
		Illegal: `\S`,
		Contains: []*Mode{
			response,
			request,
			Variant(header, func(m *Mode) { m.HasRelevance = true; m.Relevance = 0 }),
		},
	}
}
