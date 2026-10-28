package highlight

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252) highlight.js compiles JavaScript regex sources. This
// snapshot covers every compilable compiled pattern against Unicode inputs and
// the Annex B escape, class and quantifier forms where JavaScript differs from
// Go's regexp syntax. Indices are UTF-16 code units.
func TestJavaScriptRegexSources(t *testing.T) {
	data, err := os.ReadFile("testdata/js-regex/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Source string `json:"source"`
		Flags  string `json:"flags"`
		Inputs []struct {
			Input   string `json:"input"`
			Indices []int  `json:"indices"`
		} `json:"inputs"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		re, err := compilePattern(tc.Source, tc.Flags)
		if err != nil {
			t.Errorf("%q: %v", tc.Source, err)
			continue
		}
		for _, in := range tc.Inputs {
			encoded := encodeCodeUnitsString(in.Input)
			if got := codeUnitIndices(encoded, re.FindStringSubmatchIndex(encoded)); !equalInts(got, in.Indices) {
				t.Errorf("%q on %q: indices = %v; want %v", tc.Source, in.Input, got, in.Indices)
			}
		}
	}
}
