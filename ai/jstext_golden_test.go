package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// Replays the Node-generated fixture for the JavaScript text and number
// helpers: Number.prototype.toFixed and String(number) across the fixed and
// exponential ranges, toLowerCase with its full mappings (final sigma, İ),
// String.prototype.slice over negative, astral and out-of-range indices, and
// String.prototype.trim over the ECMAScript whitespace class.
func TestJSTextMatchesJavaScript(t *testing.T) {
	data, err := os.ReadFile("testdata/js-text/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Fixed []struct {
			N   float64 `json:"n"`
			D   int     `json:"d"`
			Out string  `json:"out"`
		} `json:"fixed"`
		Strings []struct {
			N   float64 `json:"n"`
			Out string  `json:"out"`
		} `json:"strings"`
		Lowers []struct {
			S   string `json:"s"`
			Out string `json:"out"`
		} `json:"lowers"`
		Slices []struct {
			S   string `json:"s"`
			A   int    `json:"a"`
			B   int    `json:"b"`
			Out string `json:"out"`
		} `json:"slices"`
		Trims []struct {
			S   string `json:"s"`
			Out string `json:"out"`
		} `json:"trims"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	total := len(corpus.Fixed) + len(corpus.Strings) + len(corpus.Lowers) + len(corpus.Slices) + len(corpus.Trims)
	if total == 0 {
		t.Fatal("empty js-text golden")
	}
	for _, testCase := range corpus.Fixed {
		if got := JSNumberToFixed(testCase.N, testCase.D); got != testCase.Out {
			t.Errorf("toFixed(%v, %d) = %q, want %q", testCase.N, testCase.D, got, testCase.Out)
		}
	}
	for _, testCase := range corpus.Strings {
		if got := JSNumberToString(testCase.N); got != testCase.Out {
			t.Errorf("String(%v) = %q, want %q", testCase.N, got, testCase.Out)
		}
	}
	for _, testCase := range corpus.Lowers {
		if got := JSLower(testCase.S); got != testCase.Out {
			t.Errorf("toLowerCase(%q) = %q, want %q", testCase.S, got, testCase.Out)
		}
	}
	for _, testCase := range corpus.Slices {
		if got := JSSlice(testCase.S, testCase.A, testCase.B); got != testCase.Out {
			t.Errorf("slice(%q, %d, %d) = %q, want %q", testCase.S, testCase.A, testCase.B, got, testCase.Out)
		}
	}
	for _, testCase := range corpus.Trims {
		if got := JSTrim(testCase.S); got != testCase.Out {
			t.Errorf("trim(%q) = %q, want %q", testCase.S, got, testCase.Out)
		}
	}
}
