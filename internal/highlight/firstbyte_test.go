package highlight

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The first-byte set is a safe over-approximation: a byte that can begin a
// non-empty match must be in it, or the engine's per-byte dispatch skips a real
// match. The JavaScript corpus provides the patterns and inputs, and the port's
// own compiled matcher supplies the matches, so this checks the set against
// everything the corpus can match without trusting the recorded JS indices.
func TestRuleFirstByteSetNeverExcludesAMatch(t *testing.T) {
	data, err := os.ReadFile("testdata/js-regex/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Source string `json:"source"`
		Flags  string `json:"flags"`
		Inputs []struct {
			Input string `json:"input"`
		} `json:"inputs"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, tc := range cases {
		set, usable := ruleFirstByteSet(preparePattern(tc.Source), strings.Contains(tc.Flags, "i"))
		if !usable {
			continue
		}
		re, err := compilePattern(tc.Source, tc.Flags)
		if err != nil {
			continue
		}
		for _, in := range tc.Inputs {
			encoded := encodeCodeUnitsString(in.Input)
			for _, match := range re.FindAllStringIndex(encoded, -1) {
				if match[0] == match[1] {
					continue
				}
				checked++
				if strings.IndexByte(set, encoded[match[0]]) < 0 {
					t.Fatalf("%q on %q: match starts with byte %#x, which the set %q omits",
						tc.Source, in.Input, encoded[match[0]], set)
				}
			}
		}
	}
	if checked < 1000 {
		t.Fatalf("only %d matches checked", checked)
	}
}

func TestMinMatchLenAndLeadingContext(t *testing.T) {
	lengths := map[string]int{
		"a": 1, "ab": 2, "a*": 0, "a+": 1, "a?": 0, "(?:ab|a)": 1, "(?:ab|abc)": 2,
		"a{2,3}": 2, "a{0,3}": 0, "": 0, "\\b": 0, "\\bword": 4, "(a)(b)": 2,
	}
	for source, want := range lengths {
		parsed := parsePattern(source)
		if parsed == nil {
			t.Fatalf("%q did not parse", source)
		}
		if got := minMatchLen(parsed); got != want {
			t.Errorf("minMatchLen(%q) = %d, want %d", source, got, want)
		}
	}
	context := map[string]bool{
		"\\bword": true, "\\Bword": true, "^word": true, "\\Aword": true,
		"(?:^|x)word": true, "\\s*\\bword": true, "word\\b": false, "a\\b": false,
		"word": false, "(?:a|b)": false,
	}
	for source, want := range context {
		parsed := parsePattern(source)
		if parsed == nil {
			t.Fatalf("%q did not parse", source)
		}
		if got := leadingContextSensitive(parsed); got != want {
			t.Errorf("leadingContextSensitive(%q) = %v, want %v", source, got, want)
		}
	}
}
