package jsdiff

import (
	"encoding/json"
	"os"
	"testing"
)

// TestDiffComponentsMatchesPinnedJsdiff replays the pinned jsdiff 8 fixture, so
// the port's component runs match exactly what edit-diff.ts renders (D87).
func TestDiffComponentsMatchesPinnedJsdiff(t *testing.T) {
	data, err := os.ReadFile("testdata/parity/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Old   []string `json:"old"`
		New   []string `json:"new"`
		Parts []struct {
			Count   int  `json:"count"`
			Added   bool `json:"added"`
			Removed bool `json:"removed"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("fixture is empty")
	}
	for index, testCase := range cases {
		got := DiffComponents(testCase.Old, testCase.New, func(a, b string) bool { return a == b })
		if len(got) != len(testCase.Parts) {
			t.Fatalf("case %d old=%v new=%v: components=%v, want %v", index, testCase.Old, testCase.New, got, testCase.Parts)
		}
		for part := range testCase.Parts {
			want := Component{Count: testCase.Parts[part].Count, Added: testCase.Parts[part].Added, Removed: testCase.Parts[part].Removed}
			if got[part] != want {
				t.Fatalf("case %d old=%v new=%v: part %d=%v, want %v", index, testCase.Old, testCase.New, part, got[part], want)
			}
		}
	}
}
