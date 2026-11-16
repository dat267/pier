package ai

import "testing"

// D487: ParseStreamingJSONText keeps the preview rules the streaming adapters
// need, which are more tolerant than the reference's parseStreamingJson on
// malformed text. The reference has no caller of that function (grep of
// packages/ai/src finds it in comments only), so the difference is documented
// here rather than split into a second parser.
func TestParseStreamingJSONTextKeepsItsTolerance(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// A missing comma is tolerated, where the reference drops everything
		// after the gap.
		{`{"a":1 "b":2}`, `{"a":1}`},
		// An invalid escape keeps the text, where the reference gives {}.
		{`{"bad":"\d"`, `{"bad":""}`},
		// A loose number is accepted, where the reference gives {}.
		{`{"p":.5}`, `{"p":0.5}`},
		{`{"plus":+1}`, `{"plus":1}`},
		// An unterminated string keeps its trailing spaces, where the reference
		// trims them.
		{`{"s":"abc   `, `{"s":"abc   "}`},
		// NaN and Infinity prefixes yield {}, where partial-json yields null.
		{`{"n":NaN}`, `{}`},
		// A complete object is returned as written.
		{`{"ok":1}`, `{"ok":1}`},
	}
	for _, testCase := range cases {
		if got := string(ParseStreamingJSONText(testCase.input)); got != testCase.want {
			t.Errorf("ParseStreamingJSONText(%q) = %s, want %s", testCase.input, got, testCase.want)
		}
	}
}
