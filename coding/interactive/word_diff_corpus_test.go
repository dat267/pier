package interactive

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.5.0 (f9a26365) components/diff.ts uses jsdiff 8.0.4 diffWords. The
// generated corpus varies whitespace, Unicode, emoji, combining marks and
// punctuation so the tokenizer and whitespace rebalancing stay byte-identical.
func TestDiffWordsRandomCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/word-diff/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Old     string       `json:"old"`
		New     string       `json:"new"`
		Changes []DiffChange `json:"changes"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) < 300 {
		t.Fatalf("corpus has %d cases", len(fixtures))
	}
	for index, fixture := range fixtures {
		got := DiffWords(fixture.Old, fixture.New)
		if len(got) != len(fixture.Changes) {
			t.Fatalf("case %d %q -> %q: %d changes, want %d\n got %#v\nwant %#v", index, fixture.Old, fixture.New, len(got), len(fixture.Changes), got, fixture.Changes)
		}
		for i := range got {
			if got[i] != fixture.Changes[i] {
				t.Fatalf("case %d %q -> %q change %d = %#v, want %#v", index, fixture.Old, fixture.New, i, got[i], fixture.Changes[i])
			}
		}
	}
}
