package coding

import (
	"encoding/json"
	"os"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), utils/ansi.ts (`stripAnsi`,
// `splitIncompleteAnsiSuffix`). The corpus is that of test/ansi-utils.test.ts
// `getCompatibilityInputs` (an escape-flavoured arbitrary corpus, including
// unterminated OSC, DCS/SOS/PM/APC strings, 8-bit CSI and single-byte ESC
// sequences) plus the cases that test asserts directly for issue 11377:
// private-parameter CSI (`ESC [ < … M`, which real terminals send for a mouse
// press) and bare keypad-mode ESC sequences, where the digits that follow must
// stay.
//
// Regenerate: node --experimental-strip-types testdata/ansi-compat/generate.mjs ../pi > testdata/ansi-compat/golden.json
func TestAnsiCompatMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/ansi-compat/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input    string `json:"input"`
		Strip    string `json:"strip"`
		Complete string `json:"complete"`
		Pending  string `json:"pending"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 200 {
		t.Fatalf("fixture has %d cases, expected the full corpus", len(cases))
	}
	for _, testCase := range cases {
		if got := StripAnsi(testCase.Input); got != testCase.Strip {
			t.Errorf("StripAnsi(%q) = %q, want %q", testCase.Input, got, testCase.Strip)
		}
		complete, pending := splitIncompleteAnsiSuffix(testCase.Input)
		if complete != testCase.Complete || pending != testCase.Pending {
			t.Errorf("splitIncompleteAnsiSuffix(%q) = %q/%q, want %q/%q",
				testCase.Input, complete, pending, testCase.Complete, testCase.Pending)
		}
	}
}
