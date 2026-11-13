package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// While tool-call arguments stream, the UI shows the partial value
// ParseStreamingJSONText produces for the bytes received so far, so every
// prefix has to decode: an unparsable partial would render as broken JSON and
// could reach a caller as arguments. The corpus covers the shapes that break
// hand-written repairs (deep nesting, strings with raw NUL and malformed UTF-8,
// lone surrogate escapes, duplicate keys, huge numbers and a large string).
func TestStreamingJSONPartialIsAlwaysValidJSON(t *testing.T) {
	documents := []string{
		`{"a":1,"b":[1,2,{"c":"x"}],"d":{"e":null}}`,
		`{"path":"/tmp/x","content":"line\nline","n":-1.5e10}`,
		`[1,2,3,{"k":"v"}]`,
		`{"s":"\ud83d\ude00 \u00e9 \t \\ \" end"}`,
		`{"nested":{"deep":{"deeper":[true,false,null,{"x":1}]}}}`,
		`{"":"","k":"","arr":[[],{},[[]]]}`,
		`{"big":1e308,"tiny":1e-308,"zero":0,"neg":-0}`,
		`{"unicode":"漢字👨‍👩‍👧","esc":"\u0000\u001f"}`,
		`{}`,
		`[]`,
		`{"incomplete":`,
		`{"trailing":1,`,
		strings.Repeat(`{"a":`, 500) + `1` + strings.Repeat(`}`, 500),
		"{\"k\":\"\x00\"}",
		"{\"k\":\"\xff\xfe\"}",
		`{"dup":1,"dup":2}`,
		`{"k":"` + strings.Repeat("x", 20000) + `"}`,
	}
	for _, document := range documents {
		step := 1
		if len(document) > 2000 {
			step = 97
		}
		for cut := 0; cut <= len(document); cut += step {
			partial := ParseStreamingJSONText(document[:cut])
			if len(partial) == 0 {
				continue
			}
			if !json.Valid(partial) {
				t.Fatalf("prefix %d of %q emitted invalid JSON %q", cut, document, partial)
			}
			var value any
			if err := json.Unmarshal(partial, &value); err != nil {
				t.Fatalf("prefix %d of %q: %v", cut, document, err)
			}
		}
	}
}
