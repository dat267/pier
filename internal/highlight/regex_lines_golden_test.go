package highlight

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// Replays the JavaScript semantics fixture generated from Node's RegExp:
// highlight.js compiles every grammar pattern with the m flag, so ^ and $ match
// at \r, \u2028 and \u2029 as line terminators too and . excludes all four, and
// the corpus also carries the Annex B escapes (\uXXXX, \xHH, bare \u/\x),
// brace quantifiers, empty classes and class-escape hyphens patternjs.go
// normalizes. The corpus records matched substrings, not offsets, so the port's
// byte indices and JavaScript's UTF-16 indices cannot disguise a semantic
// difference.
func TestPatternRegexpMatchesJavaScriptSemantics(t *testing.T) {
	data, err := os.ReadFile("testdata/regex-lines/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Pattern string  `json:"pattern"`
		Flags   string  `json:"flags"`
		Input   string  `json:"input"`
		Match   *string `json:"match"`
		All     []struct {
			Text  string `json:"text"`
			Start int    `json:"start"`
			End   int    `json:"end"`
		} `json:"all"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty regex-lines golden")
	}
	for index, testCase := range cases {
		re, err := compilePattern(testCase.Pattern, testCase.Flags)
		if err != nil {
			t.Fatalf("case %d: compile %q: %v", index, testCase.Pattern, err)
		}
		text := encodeCodeUnitsString(testCase.Input)
		var gotMatch *string
		if match := re.FindStringSubmatchIndex(text); match != nil {
			substring := text[match[0]:match[1]]
			gotMatch = &substring
		}
		if (gotMatch == nil) != (testCase.Match == nil) ||
			(gotMatch != nil && *gotMatch != *testCase.Match) {
			t.Fatalf("case %d %#v on %#v: match = %s, want %s", index, testCase.Pattern, testCase.Input,
				describe(gotMatch), describe(testCase.Match))
		}

		// The reference's matchAll reports an empty match at the previous
		// match's end; the port keeps Go's FindAll rule and skips it, because an
		// empty match paints nothing and the one production caller matches
		// keywords (internal/highlight/engine.go). Drop those from the
		// expectation instead of asserting a difference that cannot show.
		var wantAll []string
		previousEnd := -1
		for _, match := range testCase.All {
			if match.Text == "" && match.Start == previousEnd {
				continue
			}
			wantAll = append(wantAll, match.Text)
			previousEnd = match.End
		}
		var gotAll []string
		for _, match := range re.FindAllStringIndex(text, -1) {
			gotAll = append(gotAll, text[match[0]:match[1]])
		}
		if len(gotAll) == 0 && len(wantAll) == 0 {
			continue
		}
		if !reflect.DeepEqual(gotAll, wantAll) {
			t.Fatalf("case %d %#v/%q on %#v: all = %q, want %q",
				index, testCase.Pattern, testCase.Flags, testCase.Input, gotAll, wantAll)
		}
	}
}

func describe(value *string) string {
	if value == nil {
		return "<nil>"
	}
	return *value
}
