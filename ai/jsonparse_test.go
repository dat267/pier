package ai

import (
	"strings"
	"testing"
)

// Upstream v1.3.0 parseToolCallArguments (#10944): a finished tool call whose
// complete arguments text is not a JSON object keeps no parsed prefix; the
// arguments become {} and the error carries the text the model sent.
// Upstream v1.3.0 json-parse.test.ts (#10868, commit 4b994d5e5): an invalid
// \u escape such as \usepackage is repaired to literal text instead of
// failing the parse and letting the partial-json fallback drop the field.
func TestParseStreamingJSONInvalidUnicodeEscapes(t *testing.T) {
	cases := []struct {
		raw  string
		want map[string]any
	}{
		{`{"text":"\url{x}"}`, map[string]any{"text": `\url{x}`}},
		{
			`{"path":"a.tex","content":"\usepackage{x}","mode":"w"}`,
			map[string]any{"path": "a.tex", "content": `\usepackage{x}`, "mode": "w"},
		},
		// A valid \u escape still decodes.
		{`{"text":"caf\u00e9 \url"}`, map[string]any{"text": "caf\u00e9 \\url"}},
		// A \u escape cut off mid-stream drops only the incomplete tail.
		{`{"text":"ab\u00`, map[string]any{"text": "ab"}},
	}
	for _, testCase := range cases {
		var got map[string]any
		parseStreamingJSONInto(testCase.raw, &got)
		if !equalJSONMaps(got, testCase.want) {
			t.Errorf("parseStreamingJSONInto(%s) = %#v, want %#v", testCase.raw, got, testCase.want)
		}
	}
}

func TestParseToolCallArguments(t *testing.T) {
	valid, errText := ParseToolCallArguments(`{"a":1}`)
	if errText != "" || string(valid) != `{"a":1}` {
		t.Fatalf("valid = %s, %q", valid, errText)
	}
	empty, errText := ParseToolCallArguments("")
	if errText != "" || string(empty) != `{}` {
		t.Fatalf("empty = %s, %q", empty, errText)
	}
	for _, malformed := range []string{`{`, `{"a":}`, `[1,2]`, `"text"`, `null`, `not json`} {
		args, errText := ParseToolCallArguments(malformed)
		if errText == "" {
			t.Errorf("ParseToolCallArguments(%q) did not report an error", malformed)
			continue
		}
		if string(args) != `{}` {
			t.Errorf("ParseToolCallArguments(%q) arguments = %s; want {}", malformed, args)
		}
		if !strings.Contains(errText, "Received arguments:\n"+malformed) {
			t.Errorf("ParseToolCallArguments(%q) error = %q", malformed, errText)
		}
	}
}
