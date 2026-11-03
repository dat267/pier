package ai

import (
	"strings"
	"testing"
)

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
