package coding

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 core/tools/edit-diff.ts applyEditsToNormalizedContent, executed by
// the pinned module. Regenerate:
// node coding/testdata/edit-apply/generate.mjs ./pi > coding/testdata/edit-apply/golden.json
func TestApplyEditsMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/edit-apply/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string `json:"name"`
		Content  string `json:"content"`
		Edits    []Edit `json:"edits"`
		Expected struct {
			BaseContent *string `json:"baseContent"`
			NewContent  *string `json:"newContent"`
			Error       *string `json:"error"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty edit-apply corpus")
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			applied, err := ApplyEditsToNormalizedContent(test.Content, test.Edits, "f.txt")
			if test.Expected.Error != nil {
				if err == nil {
					t.Fatalf("apply succeeded with %+v, want error %q", applied, *test.Expected.Error)
				}
				if err.Error() != *test.Expected.Error {
					t.Errorf("error = %q, want %q", err.Error(), *test.Expected.Error)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error %q", err.Error())
			}
			if applied.BaseContent != *test.Expected.BaseContent || applied.NewContent != *test.Expected.NewContent {
				t.Errorf("applied = (%q, %q), want (%q, %q)",
					applied.BaseContent, applied.NewContent, *test.Expected.BaseContent, *test.Expected.NewContent)
			}
		})
	}
}
