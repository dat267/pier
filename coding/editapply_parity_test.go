package coding

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// pi core/tools/edit-diff.ts applyEditsToNormalizedContent, executed by the
// pinned module (v1.5.0, f9a26365; the original 28 cases still regenerate byte
// for byte at that pin). Regenerate:
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
	if len(cases) < 90 {
		t.Fatalf("edit-apply corpus holds %d cases", len(cases))
	}
	appliedCount, fuzzyCount, refusals := 0, 0, map[string]bool{}
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
				refusals[*test.Expected.Error] = true
				return
			}
			if err != nil {
				t.Fatalf("unexpected error %q", err.Error())
			}
			appliedCount++
			if applied.BaseContent != *test.Expected.BaseContent || applied.NewContent != *test.Expected.NewContent {
				t.Errorf("applied = (%q, %q), want (%q, %q)",
					applied.BaseContent, applied.NewContent, *test.Expected.BaseContent, *test.Expected.NewContent)
			}
			for _, edit := range test.Edits {
				if !strings.Contains(test.Content, edit.OldText) {
					fuzzyCount++
					break
				}
			}
		})
	}
	// The corpus has to keep exercising both matchers and every refusal, or it
	// decays into a few identity cases.
	if appliedCount < 30 {
		t.Errorf("only %d cases apply", appliedCount)
	}
	if fuzzyCount == 0 {
		t.Error("no applied case reaches the fuzzy matcher")
	}
	if len(refusals) < 10 {
		t.Errorf("the corpus exercises %d distinct refusals", len(refusals))
	}
	for _, needle := range []string{"Could not find", "occurrences", "overlap", "must not be empty", "No changes made"} {
		found := false
		for message := range refusals {
			if strings.Contains(message, needle) {
				found = true
			}
		}
		if !found {
			t.Errorf("no refusal mentions %q", needle)
		}
	}
}
