package oauth

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.5.2 (b4afdddf) packages/mcp/src/oauth/discovery.ts builds its
// well-known URLs from WHATWG `URL.pathname`, which drops dot segments while
// parsing, including the `%2e` spellings. The corpus is Node's own output for a
// path matrix; generate.mjs regenerates it.
func TestNormalizeDotSegmentsMatchesWHATWG(t *testing.T) {
	raw, err := os.ReadFile("testdata/dot-segments/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Path     string `json:"path"`
			Pathname string `json:"pathname"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) == 0 {
		t.Fatal("empty dot-segment fixture")
	}
	for _, row := range fixture.Rows {
		if got := normalizeDotSegments(row.Path); got != row.Pathname {
			t.Errorf("normalizeDotSegments(%q) = %q, want %q", row.Path, got, row.Pathname)
		}
	}
}
