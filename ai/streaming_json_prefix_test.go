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
	}
	for _, testCase := range cases {
		if got := string(ParseStreamingJSONText(testCase.prefix)); got != testCase.want {
			t.Errorf("%q = %s, want %s", testCase.prefix, got, testCase.want)
		}
	}
}
