package ai

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

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
