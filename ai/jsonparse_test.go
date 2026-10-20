package ai

import (
	"encoding/json"
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

// Upstream parses a finished tool call with JSON.parse and later re-stringifies
// the object, so the model's key order survives. A map round-trip sorted it.
func TestParseToolCallArgumentsPreservesKeyOrder(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"z":1,"a":2}`, `{"z":1,"a":2}`},
		{`{"path":"a","content":"b","mode":"w"}`, `{"path":"a","content":"b","mode":"w"}`},
		{`{"outer":{"z":1,"a":2},"list":[{"b":1,"a":2}]}`, `{"outer":{"z":1,"a":2},"list":[{"b":1,"a":2}]}`},
		// Numbers and whitespace normalize like JSON.parse then JSON.stringify.
		{`{ "n": 1.0 , "m": 1e2 }`, `{"n":1,"m":100}`},
	}
	for _, testCase := range cases {
		got, errText := ParseToolCallArguments(testCase.raw)
		if errText != "" || string(got) != testCase.want {
			t.Errorf("ParseToolCallArguments(%s) = %s, %q; want %s", testCase.raw, got, errText, testCase.want)
		}
	}
}

func TestParseStreamingArgsPreservesKeyOrder(t *testing.T) {
	for _, input := range []string{
		`{"z":1,"a":2}`,
		`{"outer":{"z":1,"a":2},"list":[{"b":1,"a":2}]}`,
		// A truncated tail keeps the completed prefix in order.
		`{"z":1,"a":2,"tail":`,
	} {
		got := string(parseStreamingArgs(json.RawMessage(input)))
		if !strings.Contains(got, `"z":1,"a":2`) {
			t.Errorf("parseStreamingArgs(%s) = %s; key order not preserved", input, got)
		}
	}
}

// Reference: pi v1.3.0 utils/json-parse.ts returns JSON.parse objects. Their
// JSON.stringify form enumerates array-index keys first, normalizes -0 to 0,
// and serializes overflowed numeric values as null.
func TestToolArgumentsMatchJavaScriptObjectEnumeration(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"z":0,"10":10,"2":2,"a":1}`, `{"2":2,"10":10,"z":0,"a":1}`},
		{`{"4294967295":1,"4294967294":2,"01":3,"0":4,"-0":5}`, `{"0":4,"4294967294":2,"4294967295":1,"01":3,"-0":5}`},
		{`{"n":-0,"nested":{"9":9,"3":3,"z":0}}`, `{"n":0,"nested":{"3":3,"9":9,"z":0}}`},
		{`{"n":1e400,"negative":-1e400}`, `{"n":null,"negative":null}`},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got, errText := ParseToolCallArguments(tc.raw)
			if errText != "" || string(got) != tc.want {
				t.Fatalf("finished = %s error=%q want=%s", got, errText, tc.want)
			}
			if got := parseStreamingJSONObject(tc.raw); string(got) != tc.want {
				t.Fatalf("streaming = %s want=%s", got, tc.want)
			}
		})
	}
}
