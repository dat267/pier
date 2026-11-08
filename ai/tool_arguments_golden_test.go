package ai

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) packages/ai/src/utils/json-parse.ts
// `parseToolCallArguments`. Its `arguments` object is what a finished tool call
// persists in the session and sends back to the provider, so the golden pins
// the reference's JSON.stringify bytes for each input: key order (array-index
// keys first, duplicates at their first position), JavaScript number
// formatting, -0 and overflow to null, `__proto__` as an own member, escapes
// and the repair path. `error` records only whether the reference reported an
// argumentsError; its text is Go's, not V8's (D221).
// Regenerate: node --experimental-strip-types ai/testdata/tool-arguments/generate.mjs ./pi > ai/testdata/tool-arguments/golden.json
func TestParseToolCallArgumentsMatchesReference(t *testing.T) {
	raw, err := os.ReadFile("testdata/tool-arguments/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input     string `json:"input"`
		Arguments string `json:"arguments"`
		Error     bool   `json:"error"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 200 {
		t.Fatalf("golden holds %d cases", len(cases))
	}
	errors, normalized := 0, 0
	for _, testCase := range cases {
		arguments, errText := ParseToolCallArguments(testCase.Input)
		want := replaceLoneSurrogateEscapes(testCase.Arguments)
		if want != testCase.Arguments {
			normalized++
		}
		if string(arguments) != want {
			t.Errorf("ParseToolCallArguments(%q) arguments = %s; want %s", testCase.Input, arguments, want)
		}
		if (errText != "") != testCase.Error {
			t.Errorf("ParseToolCallArguments(%q) error = %q; reference error presence is %v", testCase.Input, errText, testCase.Error)
		}
		if testCase.Error {
			errors++
		}
	}
	// D253/D363: Go strings cannot hold an unpaired surrogate, so pier writes
	// U+FFFD where JSON.stringify keeps the escape. The golden records the
	// reference's bytes, and the cases that need that one normalization must
	// exist, or the helper above would be untested.
	if normalized == 0 {
		t.Fatal("no golden case exercises the lone-surrogate normalization (D253/D363)")
	}
	// Both branches have to stay exercised, so a one-sided corpus cannot pass.
	if errors == 0 || errors == len(cases) {
		t.Fatalf("%d of %d cases error; the corpus covers one branch only", errors, len(cases))
	}
	// The golden must keep the formatting rules that only a reparse can break.
	for _, probe := range []string{`"2":2,"10":10`, `"n":0,`, `"n":null`, `{"a":3,"b":2}`, `"__proto__"`} {
		found := false
		for _, testCase := range cases {
			if strings.Contains(testCase.Arguments, probe) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no golden case exercises %s", probe)
		}
	}
}

// replaceLoneSurrogateEscapes turns each lone surrogate escape in JSON text into
// U+FFFD, the byte pier writes for it (D253/D363). A valid pair is left alone;
// an escaped backslash before a "u" is literal text, not an escape.
func replaceLoneSurrogateEscapes(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	for index := 0; index < len(text); index++ {
		char := text[index]
		if char != '\\' || index+1 >= len(text) {
			out.WriteByte(char)
			continue
		}
		next := text[index+1]
		if next != 'u' || index+5 >= len(text) {
			// Any other escape, including \\, is copied with its introducer.
			out.WriteByte(char)
			continue
		}
		unit, ok := parseHexUnit(text[index+2 : index+6])
		if !ok {
			out.WriteByte(char)
			continue
		}
		if unit >= 0xd800 && unit < 0xdc00 && index+11 < len(text) && text[index+6] == '\\' && text[index+7] == 'u' {
			if low, ok := parseHexUnit(text[index+8 : index+12]); ok && low >= 0xdc00 && low < 0xe000 {
				out.WriteString(text[index : index+12])
				index += 11
				continue
			}
		}
		if unit >= 0xd800 && unit < 0xe000 {
			out.WriteString("\ufffd")
			index += 5
			continue
		}
		out.WriteString(text[index : index+6])
		index += 5
	}
	return out.String()
}

func parseHexUnit(text string) (uint32, bool) {
	var value uint32
	for index := 0; index < len(text); index++ {
		char := text[index]
		switch {
		case char >= '0' && char <= '9':
			value = value<<4 | uint32(char-'0')
		case char >= 'a' && char <= 'f':
			value = value<<4 | uint32(char-'a'+10)
		case char >= 'A' && char <= 'F':
			value = value<<4 | uint32(char-'A'+10)
		default:
			return 0, false
		}
	}
	return value, true
}
