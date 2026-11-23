package highlight

import "strings"

func init() { RegisterLanguage("http", HTTP()) }

// The response and request rules open with a zero-width lookahead at the line
// start; RE2 has none, so each becomes a beforeBegin guard.
//
// The version and request guards are hand-written. An `\A`-anchored regexp that
// Go cannot prefilter costs a full pass over the remaining input per call, and
// these run at every candidate: measured 119 us and 121 us on a 24 KiB tail,
// which is D526's defect in this language.
var httpHeaderGuard = mustCompileJS(`\A:\s`)

// httpVersionGuard ports `\AHTTP/(2|1\.[01]) \d{3}`: the version prefix, then a
// space and three digits.
func httpVersionGuard(s string) bool {
	rest, ok := cutHTTPVersionPrefix(s)
	if !ok || rest < len(s) && s[rest] != ' ' {
		return false
	}
	rest++
	for i := 0; i < 3; i++ {
		if rest+i >= len(s) || s[rest+i] < '0' || s[rest+i] > '9' {
			return false
		}
	}
	return true
}

// httpRequestGuard ports `\A[A-Z]+ .* HTTP/(2|1\.[01])$`: an upper-case method,
// a space, anything, then a space and the version at the end of the input.
func httpRequestGuard(s string) bool {
	method := 0
	for method < len(s) && s[method] >= 'A' && s[method] <= 'Z' {
		method++
	}
	if method == 0 || method >= len(s) || s[method] != ' ' {
		return false
	}
	// `.* HTTP/...` at the end: the last " HTTP/" must be followed by a version
	// and nothing else.
	idx := strings.LastIndex(s, " HTTP/")
	if idx < method {
		return false
	}
	end, ok := cutHTTPVersionPrefix(s[idx+1:])
	return ok && idx+1+end == len(s)
}

// cutHTTPVersionPrefix consumes `HTTP/2` or `HTTP/1.0` or `HTTP/1.1` at the
// start of s and returns the offset after it.
func cutHTTPVersionPrefix(s string) (int, bool) {
	if !strings.HasPrefix(s, "HTTP/") {
		return 0, false
	}
	switch {
	case len(s) >= 6 && s[5] == '2':
		return 6, true
	case len(s) >= 8 && s[5] == '1' && s[6] == '.' && (s[7] == '0' || s[7] == '1'):
		return 8, true
	}
	return 0, false
}

// HTTP returns the HTTP language definition (a port of languages/http.js).
func HTTP() *Language {
	const version = `HTTP/(2|1\.[01])`
	const headerName = `[A-Za-z][A-Za-z0-9-]*`

	header := &Mode{ClassName: "attribute", Begin: `^` + headerName}
	header.beforeBegin = func(m *match, resp *Response) {
		if !httpHeaderGuard.MatchStringWS(m.input[m.index+len(m.lexeme):], m.jsLines) {
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
		if !httpVersionGuard(m.input[m.index+len(m.lexeme):]) {
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
		if !httpRequestGuard(m.input[m.index+len(m.lexeme):]) {
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
