package ai

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
)

func TestCanonicalJSONIsNormalized(t *testing.T) {
	normalized := []string{
		`{}`, `[]`, `{"a":1,"b":[true,null,"x"]}`,
		`{"0":1,"1":2,"a":3}`,
		`{"a":1,"b":2,"c":3,"d":4,"e":5,"f":6,"g":7,"h":8,"i":9,"j":10,"k":11,"l":12,"m":13,"n":14,"o":15,"p":16,"q":17}`, // wide-object map path
		`"\u0001"`, `"\b\f\n\r\t"`, `1.5`, `1e-7`, "\"\u2028\"", "\"\x7f\"",
	}
	for _, input := range normalized {
		if !canonicalJSONIsNormalized([]byte(input)) {
			t.Errorf("canonicalJSONIsNormalized(%q) = false", input)
		}
	}
	noncanonical := []string{
		` null`, `{"a": 1}`, `{"a":1,"a":2}`,
		`{"a":1,"1":2}`, `{"2":1,"1":2}`, `{"1":"\u0031"}`,
		`"\u000A"`, `"\u003c"`, `"\/"`, `"\u2028"`,
		`01`, `1.0`, `-0`, `1e2`, `1E2`, "\"\xff\"", ``,
	}
	for _, input := range noncanonical {
		if canonicalJSONIsNormalized([]byte(input)) {
			t.Errorf("canonicalJSONIsNormalized(%q) = true", input)
		}
	}
}

func TestCanonicalizeNormalizedFastPathOwnsItsBytes(t *testing.T) {
	input := []byte(`{"a":"<&>","b":[1,1.5]}`)
	got, err := canonicalJavaScriptJSON(input)
	if err != nil || string(got) != string(input) {
		t.Fatalf("canonical = %q/%v, want %q", got, err, input)
	}
	got[0] = '['
	if input[0] != '{' {
		t.Fatal("fast path returned the caller's buffer instead of an owned copy")
	}
}

// JSON.parse rejects trailing non-whitespace; the scanner used to truncate to
// the first value (the raw-message path already validated first).
func TestCanonicalizeRejectsTrailingText(t *testing.T) {
	for _, input := range []string{`1 2`, `{} trailing`, `truex`, `01`, `[1] [2]`} {
		if _, err := canonicalJavaScriptJSON([]byte(input)); err == nil {
			t.Errorf("canonicalJavaScriptJSON(%q) accepted trailing text", input)
		}
	}
	if _, err := canonicalJavaScriptJSON([]byte(`1 2`)); err == nil ||
		!strings.Contains(err.Error(), "after top-level value") {
		t.Fatalf("trailing-value diagnostic = %v", err)
	}
	if got, err := canonicalJavaScriptJSON([]byte("1\n\t ")); err != nil || string(got) != "1" {
		t.Fatalf("trailing whitespace = %q/%v, want 1", got, err)
	}
}

// Reference: pi v1.4.0 (4a080252). Upstream builds request params objects and
// rewrites JSON schemas as plain JavaScript objects, so JSON.stringify orders
// array-index keys first (ascending), then the other members in first-assignment
// order. The shared ordered-object type serves both the wire builders and the
// schema rewriters.
func TestOrderedJSONFieldsMatchJavaScriptKeyOrder(t *testing.T) {
	fields, err := decodeOrderedJSONFields([]byte(`{"b":1,"2":2,"a":3,"1":4}`))
	if err != nil {
		t.Fatal(err)
	}
	// Re-assigning an existing member keeps its position; a new name appends.
	fields.set("b", []byte(`9`))
	fields.set("c", []byte(`5`))
	encoded, err := fields.marshal()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"1":4,"2":2,"b":9,"a":3,"c":5}`; got != want {
		t.Fatalf("marshal = %s, want %s", got, want)
	}

	// Removing a member keeps the remaining declaration order.
	fields.remove("a")
	encoded, err = fields.marshal()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"1":4,"2":2,"b":9,"c":5}`; got != want {
		t.Fatalf("marshal after remove = %s, want %s", got, want)
	}
}

// D468: the reference's AssistantMessage is built with a usage object, so a
// message it writes always carries one; pier models usage as a struct field, so
// a message read from a file without usage is re-serialized with the zero object
// (input/output/cacheRead/cacheWrite/totalTokens and a zero cost). Dropping it
// would need a custom marshaler over the field order that persisted sessions
// depend on, for a file upstream never writes.
func TestAssistantMessageAlwaysCarriesUsage(t *testing.T) {
	message := &AssistantMessage{
		Content:    ContentList{TextContent{Text: "hi"}},
		API:        "openai",
		Provider:   "openai",
		Model:      "gpt",
		StopReason: StopStop,
		Timestamp:  1700000000000,
	}
	encoded, err := MarshalMessage(message)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}`) {
		t.Fatalf("message = %s, want the zero usage object", encoded)
	}
}

// pi v1.4.0 API providers and session-manager.ts use JSON.stringify for
// numeric payloads too, including unpadded exponents and negative zero.
func TestMarshalJSONFormatsJavaScriptNumbers(t *testing.T) {
	for _, tt := range []struct {
		value any
		want  string
	}{
		{math.Copysign(0, -1), `0`},
		{1e-7, `1e-7`},
		{json.RawMessage(`{"n":1.0,"small":1e-07,"zero":-0.0,"text":"-0 1e-07"}`), `{"n":1,"small":1e-7,"zero":0,"text":"-0 1e-07"}`},
	} {
		got, err := MarshalJSON(tt.value)
		if err != nil || string(got) != tt.want {
			t.Errorf("value=%v got=%s error=%v; want %s", tt.value, got, err, tt.want)
		}
		if _, ok := tt.value.(json.RawMessage); !ok {
			got, err = MarshalIndentJSON(tt.value)
			if err != nil || string(got) != tt.want {
				t.Errorf("indented value=%v got=%s error=%v; want %s", tt.value, got, err, tt.want)
			}
		}
	}
}

// D253: pi v1.4.0 core/session-manager.ts JSON.stringify uses JS doubles.
// Integer fields remain representable by the port's typed decoders; rounding
// MaxInt64 through float64 would serialize a number outside the int64 range.
func TestMarshalJSONPreservesTypedIntegerRange(t *testing.T) {
	value := struct {
		Number int64 `json:"number"`
	}{math.MaxInt64}
	encoded, err := MarshalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Number int64 `json:"number"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Number != value.Number {
		t.Fatalf("encoded=%s decoded=%d error=%v", encoded, decoded.Number, err)
	}
}

// Reference: pi v1.4.0 coding-agent/core/session-manager.ts persists with
// JSON.stringify; ai API implementations also stringify request payloads.
// Node's JSON.stringify leaves U+2028/U+2029 literal, unlike encoding/json.
func TestMarshalJSONPreservesJavaScriptLineSeparators(t *testing.T) {
	for _, test := range []struct{ name, value, want string }{
		{"separators", "a\u2028b\u2029c", "\"a\u2028b\u2029c\""},
		{"literal escapes", `literal \u2028 and \u2029`, `"literal \\u2028 and \\u2029"`},
		{"backslash before separator", "\\\u2028", "\"\\\\\u2028\""},
		{"html and controls", "<&>\n\t\u2029", "\"<&>\\n\\t\u2029\""},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, marshal := range []func(any) ([]byte, error){MarshalJSON, MarshalIndentJSON} {
				got, err := marshal(test.value)
				if err != nil || string(got) != test.want {
					t.Fatalf("got=%q err=%v want=%q", got, err, test.want)
				}
				var decoded string
				if err := json.Unmarshal(got, &decoded); err != nil || decoded != test.value {
					t.Fatalf("roundtrip=%q err=%v", decoded, err)
				}
			}
		})
	}
}

// JSON.stringify applies the same escaping to nested strings and property keys.
func TestMarshalJSONNestedLineSeparators(t *testing.T) {
	value := map[string]any{"key\u2028": []string{"\u2029", `\u2029`}}
	got, err := MarshalJSON(value)
	want := "{\"key\u2028\":[\"\u2029\",\"\\\\u2029\"]}"
	if err != nil || string(got) != want {
		t.Fatalf("got=%q err=%v want=%q", got, err, want)
	}
}

// Reference: pi v1.4.0 core/session-manager.ts and agent/src/proxy.ts use
// JSON.stringify on parsed values, including imported/nested raw payloads.
func TestMarshalRawJSONCanonicalizesJavaScriptValues(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"text":"\u0061\/\u4f60\u597d"}`, `{"text":"a/你好"}`},
		{`{"10":1,"2":2,"a":3}`, `{"2":2,"10":1,"a":3}`},
		{`{"x":1,"y":0,"x":2}`, `{"x":2,"y":0}`},
		{`{"outer":[{"10":1,"2":2,"a":"\u0061"}],"n":9223372036854775807}`, `{"outer":[{"2":2,"10":1,"a":"a"}],"n":9223372036854775807}`},
	} {
		raw := json.RawMessage(tc.raw)
		got, err := MarshalJSON(raw)
		if err != nil || string(got) != tc.want {
			t.Errorf("raw=%s got=%s err=%v want=%s", tc.raw, got, err, tc.want)
		}
		// Indentation must not prevent canonicalization.
		indented, err := MarshalIndentJSON(struct {
			Data json.RawMessage `json:"data"`
		}{raw})
		if err != nil {
			t.Fatal(err)
		}
		var nested struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(indented, &nested); err != nil {
			t.Fatal(err)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, nested.Data); err != nil {
			t.Fatal(err)
		}
		if compact.String() != tc.want {
			t.Errorf("indented=%s want data=%s", indented, tc.want)
		}
	}
}

// D253: a lone surrogate escape cannot live in a Go string, so the encoder
// writes U+FFFD where JSON.stringify would preserve the escape; a valid
// surrogate pair still becomes the character.
func TestMarshalJSONReplacesLoneSurrogateEscapes(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`"a\ud800b"`, "\"a\ufffdb\""},
		{`"a\udc00b"`, "\"a\ufffdb\""},
		{`"\udc00\udc00"`, "\"\ufffd\ufffd\""},
		{`"\udc00\ud800"`, "\"\ufffd\ufffd\""},
		{`{"k":"\ud800"}`, "{\"k\":\"\ufffd\"}"},
		{`"\ud83d\ude00"`, "\"😀\""},
	} {
		got, err := MarshalJSON(json.RawMessage(tc.raw))
		if err != nil || string(got) != tc.want {
			t.Errorf("raw=%s got=%q err=%v want=%q", tc.raw, got, err, tc.want)
		}
	}
}

// Reference: pi v1.5.0 (f9a26365) packages/ai/src/utils/json-parse.ts
// `repairJson`, whose output decides what a repaired finished tool call
// carries (parseToolCallArguments parses its result). The golden holds the
// reference's output for each input.
// Regenerate: node --experimental-strip-types ai/testdata/json-repair/generate.mjs ./pi > ai/testdata/json-repair/golden.json
func TestRepairJSONMatchesReference(t *testing.T) {
	raw, err := os.ReadFile("testdata/json-repair/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input  string `json:"input"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 100 {
		t.Fatalf("golden holds %d cases", len(cases))
	}
	// Enough of the corpus must need repair that an identity golden cannot pass
	// this test.
	repaired := 0
	for _, testCase := range cases {
		if testCase.Output != testCase.Input {
			repaired++
		}
	}
	if repaired < 50 {
		t.Fatalf("only %d of %d cases need repair", repaired, len(cases))
	}
	for _, testCase := range cases {
		if got := RepairJSON(testCase.Input); got != testCase.Output {
			t.Errorf("RepairJSON(%q) = %q; want %q", testCase.Input, got, testCase.Output)
		}
	}

	// The corpus has to keep exercising every repair rule, so trimming it
	// cannot silently drop one.
	covered := map[string]bool{}
	for _, testCase := range cases {
		switch {
		case strings.Contains(testCase.Input, `\ud83d`) || strings.Contains(testCase.Input, `\udc00`):
			covered["surrogate escape"] = true
		case strings.Contains(testCase.Input, "\x01") || strings.Contains(testCase.Input, "\t"):
			covered["raw control character"] = true
		case strings.Contains(testCase.Input, `\q`) || strings.Contains(testCase.Input, `\d`):
			covered["invalid escape"] = true
		case strings.Contains(testCase.Input, `\u12"`) || strings.Contains(testCase.Input, `\u00"`):
			covered["short unicode escape"] = true
		}
	}
	for _, rule := range []string{"surrogate escape", "raw control character", "invalid escape", "short unicode escape"} {
		if !covered[rule] {
			t.Errorf("the golden covers no %s case", rule)
		}
	}
}

// JSON.stringify is the reference's serializer for transcripts and wire
// payloads. The fixture holds Node's output for a corpus of documents, compact
// and two-space indented; this compares pier's canonical marshalers with it.
// Regenerate with node ai/testdata/json-stringify/generate.mjs.
func TestMarshalJSONMatchesJavaScriptStringify(t *testing.T) {
	data, err := os.ReadFile("testdata/json-stringify/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input    string `json:"input"`
		Compact  string `json:"compact"`
		Indented string `json:"indented"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty corpus")
	}
	for _, testCase := range cases {
		compact, err := MarshalJSON(json.RawMessage(testCase.Input))
		if err != nil {
			t.Fatalf("%s: %v", testCase.Input, err)
		}
		if string(compact) != testCase.Compact {
			t.Errorf("%s compact = %s, want %s", testCase.Input, compact, testCase.Compact)
		}
		indented, err := MarshalIndentJSON(json.RawMessage(testCase.Input))
		if err != nil {
			t.Fatalf("%s: %v", testCase.Input, err)
		}
		if string(indented) != testCase.Indented {
			t.Errorf("%s indented = %s, want %s", testCase.Input, indented, testCase.Indented)
		}
	}
}
