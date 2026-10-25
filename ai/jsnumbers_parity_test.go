package ai

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

// the Number.prototype.toFixed and String(number) behaviour the port mirrors.
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
