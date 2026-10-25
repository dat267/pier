package coding

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 core/tools/edit-diff.ts generateUnifiedPatch and
// generateDiffString over jsdiff 8.0.4, executed by the pinned module.
// Regenerate:
// node coding/testdata/edit-diff-patch/generate.mjs ./pi > coding/testdata/edit-diff-patch/golden.json
func TestEditDiffPatchCorpusMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/edit-diff-patch/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name                     string `json:"name"`
		OldContent               string `json:"oldContent"`
		NewContent               string `json:"newContent"`
		ExpectedPatch            string `json:"expectedPatch"`
		ExpectedDiff             string `json:"expectedDiff"`
		ExpectedFirstChangedLine int    `json:"expectedFirstChangedLine"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty edit-diff patch corpus")
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			if got := GenerateUnifiedPatch("f.txt", test.OldContent, test.NewContent, 4); got != test.ExpectedPatch {
				t.Errorf("patch =\n%s\nwant\n%s", got, test.ExpectedPatch)
			}
			diff, first, ok := GenerateDiffString(test.OldContent, test.NewContent, 4)
			if diff != test.ExpectedDiff {
				t.Errorf("diff =\n%s\nwant\n%s", diff, test.ExpectedDiff)
			}
			wantPresent := test.ExpectedFirstChangedLine >= 0
			if ok != wantPresent || (wantPresent && first != test.ExpectedFirstChangedLine) {
				t.Errorf("firstChangedLine = (%d, %v), want (%d, %v)", first, ok, test.ExpectedFirstChangedLine, wantPresent)
			}
		})
	}
}
