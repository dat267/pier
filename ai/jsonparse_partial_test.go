package ai

import "testing"

// Reference: pi v1.4.0 utils/json-parse.ts parseStreamingJson falls back to the
// npm partial-json package, which trims the document, completes a keyword
// prefix at end of input ("tru" -> true) and keeps a number's mantissa before an
// unfinished exponent ("1e" -> 1).
func TestParseStreamingJSONCompletesPartialLiterals(t *testing.T) {
	cases := []struct {
		raw  string
		want map[string]any
	}{
		{`{"k":t`, map[string]any{"k": true}},
		{`{"k":tru`, map[string]any{"k": true}},
		{`{"k":f`, map[string]any{"k": false}},
		{`{"k":fals`, map[string]any{"k": false}},
		{`{"k":n`, map[string]any{"k": nil}},
		{`{"k":nul`, map[string]any{"k": nil}},
		// partial-json trims the whole document, so an open string's trailing
		// whitespace is not part of the value.
		{"{\"k\":\"x ", map[string]any{"k": "x"}},
		// An unfinished exponent keeps the mantissa.
		{`{"k":1e`, map[string]any{"k": float64(1)}},
		{`{"k":1.5e`, map[string]any{"k": 1.5}},
		{`{"k":12`, map[string]any{"k": float64(12)}},
	}
	for _, testCase := range cases {
		var got map[string]any
		parseStreamingJSONInto(testCase.raw, &got)
		if !equalJSONMaps(got, testCase.want) {
			t.Errorf("parseStreamingJSONInto(%s) = %#v, want %#v", testCase.raw, got, testCase.want)
		}
	}
}
