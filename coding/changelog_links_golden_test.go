package coding

import (
	"encoding/json"
	"os"
	"testing"
)

// Replays the upstream-generated changelog link cases: relative and absolute
// paths, directory targets, floating refs, the legacy repository prefix,
// external, protocol-relative, fragment-only and opaque-scheme targets, query
// and fragment suffixes, traversal, and percent-encoding of spaces, `%` and
// non-ASCII paths.
func TestChangelogLinksMatchThePin(t *testing.T) {
	data, err := os.ReadFile("testdata/changelog-links/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Label      string `json:"label"`
		Markdown   string `json:"markdown"`
		Version    string `json:"version"`
		Normalized string `json:"normalized"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty changelog-links golden")
	}
	for _, testCase := range cases {
		if got := NormalizeChangelogLinks(testCase.Markdown, testCase.Version); got != testCase.Normalized {
			t.Errorf("%s: normalized = %q, want %q", testCase.Label, got, testCase.Normalized)
		}
	}
}
