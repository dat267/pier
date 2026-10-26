package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252), ai/src/utils/hash.ts shortHash.
// Regenerate:
//
//	node --experimental-strip-types ai/testdata/short-hash/generate.mjs ./pi > ai/testdata/short-hash/golden.json
func TestShortHashMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/short-hash/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Input    string `json:"input"`
		Expected string `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty short-hash corpus")
	}
	for _, fixture := range fixtures {
		if got := ShortHash(fixture.Input); got != fixture.Expected {
			t.Errorf("ShortHash(%q) = %q, want %q", fixture.Input, got, fixture.Expected)
		}
	}
}
