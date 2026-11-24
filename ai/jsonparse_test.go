package ai

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Upstream v1.4.0 parseToolCallArguments (#10944): a finished tool call whose
// complete arguments text is not a JSON object keeps no parsed prefix; the
// arguments become {} and the error carries the text the model sent.
// Upstream v1.4.0 json-parse.test.ts (#10868, commit 4b994d5e5): an invalid
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

// Reference: pi v1.4.0 utils/json-parse.ts returns JSON.parse objects. Their
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

// D457: a lone surrogate has no Go string representation. The reference keeps
// the code unit and `JSON.stringify` writes it back as an escape, so the tool
// receives `{"a":"\ud83d"}`; the port's codec decodes it to U+FFFD. This is the
// same constraint the streaming partial values hit, and it reaches the
// completed arguments too: a model that emits half a surrogate pair in its tool
// arguments hands the tool a replacement character.
//
// TestToolCallArgumentsReplaceLoneSurrogates records the port's behavior.
func TestToolCallArgumentsReplaceLoneSurrogates(t *testing.T) {
	cases := []struct{ input, want string }{
		{`{"a":"\ud83d"}`, `{"a":"` + "\uFFFD" + `"}`},
		{`{"a":"x\ud83dy"}`, `{"a":"x` + "\uFFFD" + `y"}`},
		{`{"a":"\udc00\udc00"}`, `{"a":"` + "\uFFFD\uFFFD" + `"}`},
		// A complete pair is a single rune and survives.
		{`{"a":"\ud83d\ude00"}`, `{"a":"😀"}`},
	}
	for _, testCase := range cases {
		args, errText := ParseToolCallArguments(testCase.input)
		if errText != "" {
			t.Fatalf("%q reported %q", testCase.input, errText)
		}
		if string(args) != testCase.want {
			t.Errorf("%q = %s, want %s (D457)", testCase.input, args, testCase.want)
		}
	}
}

// TestParseStreamingEscapes pins the partial-JSON string decoder to upstream's
// parseStreamingJson (verified against the installed pi bundle + partial-json):
// complete escapes decode to their characters as soon as they arrive, while an
// invalid or truncated escape stops the string at that point.
func TestParseStreamingEscapes(t *testing.T) {
	cases := []struct {
		name string
		json string
		want map[string]any
	}{
		{"tab", `{"command":"echo\tfoo`, map[string]any{"command": "echo\tfoo"}},
		{"tabs", `{"command":"a\tb\tc`, map[string]any{"command": "a\tb\tc"}},
		{"newline", `{"command":"line1\nline2`, map[string]any{"command": "line1\nline2"}},
		{"backslash", `{"path":"C:\\temp\\x`, map[string]any{"path": `C:\temp\x`}},
		{"quote", `{"command":"quote \"q\" and tab\there`, map[string]any{"command": "quote \"q\" and tab\there"}},
		{"unicode", `{"command":"emoji \u00e9 \ud83d\ude00`, map[string]any{"command": "emoji \u00e9 \U0001F600"}},
		{"trailing backslash escape", `{"command":"trailing backslash\\`, map[string]any{"command": `trailing backslash\`}},
		{"solidus", `{"command":"x\/y`, map[string]any{"command": "x/y"}},
		{"backspace formfeed carriage", `{"command":"a\b\f\rb`, map[string]any{"command": "a\b\f\rb"}},

		// Invalid or truncated escapes stop the string (upstream drops the rest).
		{"invalid escape", `{"command":"bad \q escape`, map[string]any{"command": "bad "}},
		{"invalid unicode", `{"command":"x\uZZZZy`, map[string]any{"command": "x"}},
		{"truncated unicode", `{"command":"x\u12`, map[string]any{"command": "x"}},
		{"lone backslash", `{"command":"x\`, map[string]any{"command": "x"}},

		// Escapes in a later incomplete field still decode.
		{"with timeout", `{"command":"a\tb","timeout":30`, map[string]any{"command": "a\tb", "timeout": float64(30)}},
		{"array value", `{"command":["a\tb"],"x":1`, map[string]any{"command": []any{"a\tb"}, "x": float64(1)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			parseStreamingJSONInto(tc.json, &got)
			if !equalJSONMaps(got, tc.want) {
				t.Fatalf("parseStreamingJSONInto(%s) = %#v, want %#v", tc.json, got, tc.want)
			}
			// The renderer path re-encodes the parsed value; it must round-trip.
			var again map[string]any
			parseStreamingJSONInto(string(parseStreamingArgs(json.RawMessage(tc.json))), &again)
			if !equalJSONMaps(again, tc.want) {
				t.Fatalf("parseStreamingArgs(%s) round-trip = %#v, want %#v", tc.json, again, tc.want)
			}
		})
	}
}

func equalJSONMaps(a, b map[string]any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

// TestJSNumberFormattingMatchesJavaScript pins JSNumberToString and
// JSNumberToFixed to the String(number) and Number.prototype.toFixed
// behaviour the port mirrors.
// The corpus records JavaScript's own output for the same doubles.
// Regenerate:
// node ai/testdata/js-numbers/generate.mjs > ai/testdata/js-numbers/golden.json
func TestJSNumberFormattingMatchesJavaScript(t *testing.T) {
	raw, err := os.ReadFile("testdata/js-numbers/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Value  string   `json:"value"`
		String string   `json:"string"`
		Fixed  []string `json:"fixed"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty js-numbers corpus")
	}
	for _, test := range cases {
		value, err := strconv.ParseFloat(test.Value, 64)
		if err != nil {
			t.Fatalf("ParseFloat(%q): %v", test.Value, err)
		}
		if got := JSNumberToString(value); got != test.String {
			t.Errorf("JSNumberToString(%s) = %q, want %q", test.Value, got, test.String)
		}
		for digits, want := range test.Fixed {
			if got := JSNumberToFixed(value, digits); got != want {
				t.Errorf("JSNumberToFixed(%s, %d) = %q, want %q", test.Value, digits, got, want)
			}
		}
	}
}
