package ai

import "testing"

// Reference: pi v1.5.0 (f9a26365), utils/streaming-json.ts
// `StreamingJsonParser.snapshot`, with every expectation below produced by
// running it over the same prefix. While tool-call arguments stream, the partial
// value is what the UI shows, and a number that overflows a float is
// JavaScript's Infinity, which stringifies as null. An *unfinished* exponent is
// a different case: the reference keeps the mantissa ("1e" reports 1).
func TestStreamingJSONPartialNumberMatchesReference(t *testing.T) {
	cases := []struct{ prefix, want string }{
		{`{"big":1`, `{"big":1}`},
		{`{"big":1e`, `{"big":1}`},
		{`{"big":1e4`, `{"big":10000}`},
		{`{"big":1e40`, `{"big":1e+40}`},
		{`{"big":1e400`, `{"big":null}`},
		{`{"big":1e400,"`, `{"big":null}`},
		{`{"big":-1e400`, `{"big":null}`},
		{`{"big":1e308`, `{"big":1e+308}`},
		{`{"big":1e400}`, `{"big":null}`},
		{`{"big":2.5e400,"x":1}`, `{"big":null,"x":1}`},
		{`{"exp":1E400,"y":2`, `{"exp":null,"y":2}`},
		// An unfinished exponent keeps its mantissa whichever case it used.
		{`{"x":1E`, `{"x":1}`},
		{`{"x":1E+`, `{"x":1}`},
		{`{"x":1E+2`, `{"x":100}`},
		// An open string keeps its trailing whitespace.
		{`{"":"empty `, `{"":"empty "}`},
	}
	for _, testCase := range cases {
		if got := string(ParseStreamingJSONText(testCase.prefix)); got != testCase.want {
			t.Errorf("%q = %s, want %s", testCase.prefix, got, testCase.want)
		}
	}
}

// D457: two partial values the port approximates, both only visible while the
// arguments stream and self-correcting on the next delta. The reference keeps
// the half of a surrogate pair it has so far and stringifies it as an escape
// (`\ud83d`); a Go string cannot hold a lone surrogate, so the port writes the
// replacement character. For input the reference rejects as a number it reports
// no partial value at all, while the port salvages what it can (`01` reports 1).
// The completed arguments come from the full text in both cases, so a tool never
// sees either approximation.
//
// TestStreamingJSONPartialApproximations records the port's behavior.
func TestStreamingJSONPartialApproximations(t *testing.T) {
	cases := []struct{ prefix, want string }{
		{`{"k":"\ud83d`, "{\"k\":\"\ufffd\"}"},
		{`{"k":"\ude00`, "{\"k\":\"\ufffd\"}"},
		{`{"x":01`, `{"x":1}`},
		{`{"x":.5`, `{"x":0.5}`},
		{`{"x":1.`, `{"x":1}`},
		{`{"x":+1`, `{"x":1}`},
	}
	for _, testCase := range cases {
		if got, want := string(ParseStreamingJSONText(testCase.prefix)), testCase.want; got != want {
			t.Errorf("%q = %s, want %s (D457)", testCase.prefix, got, want)
		}
	}
}
