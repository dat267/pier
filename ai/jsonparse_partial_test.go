package ai

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

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

// The final tool-argument decoder rejects nesting beyond encoding/json's
// 10,000-level limit. Streaming previews must not bypass that limit through
// the recursive partial parser and spend unbounded time parsing the same value.
func TestParseStreamingJSONRejectsExcessiveNesting(t *testing.T) {
	const depth = 10_001
	input := `{"a":` + strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth)
	if got := string(ParseStreamingJSONText(input)); got != "{}" {
		t.Fatalf("ParseStreamingJSONText at depth %d returned %d bytes, want {}", depth, len(got))
	}
}

// Reference: pi v1.4.0 packages/ai/src/utils/json-parse.ts delegates partial
// values to partial-json; streaming parse cost should not grow quadratically.
func TestParseStreamingJSONLongEscapeRunIsEfficient(t *testing.T) {
	const escapes = 10_000
	input := `{"k":"` + strings.Repeat(`\n`, escapes)
	start := time.Now()
	raw := ParseStreamingJSONText(input)
	elapsed := time.Since(start)
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parse result is invalid JSON: %v", err)
	}
	if got["k"] != strings.Repeat("\n", escapes) {
		t.Fatalf("decoded string length = %d, want %d", len(got["k"]), escapes)
	}
	if elapsed > time.Second {
		t.Fatalf("parsing %d valid escapes took %v, want under 1s", escapes, elapsed)
	}
}
