package coding

import (
	"encoding/json"
	"os"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), utils/version-check.ts uses the `semver`
// package's valid/compare. Regenerate:
//
//	node coding/testdata/semver/generate.mjs ./pi > coding/testdata/semver/golden.json
func TestPackageVersionComparisonMatchesSemver(t *testing.T) {
	raw, err := os.ReadFile("testdata/semver/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Validity []struct {
			Input string  `json:"input"`
			Valid *string `json:"valid"`
		} `json:"validity"`
		Pairs []struct {
			Left       string `json:"left"`
			Right      string `json:"right"`
			Comparison *int   `json:"comparison"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Validity) == 0 || len(corpus.Pairs) == 0 {
		t.Fatal("empty semver corpus")
	}
	for _, fixture := range corpus.Validity {
		_, valid := ComparePackageVersions(fixture.Input, "0.0.0")
		if valid != (fixture.Valid != nil) {
			t.Errorf("validity of %q = %v, want %v", fixture.Input, valid, fixture.Valid != nil)
		}
	}
	for _, fixture := range corpus.Pairs {
		comparison, valid := ComparePackageVersions(fixture.Left, fixture.Right)
		if fixture.Comparison == nil {
			if valid {
				t.Errorf("comparison of invalid pair %q/%q was accepted", fixture.Left, fixture.Right)
			}
			continue
		}
		if !valid || comparison != *fixture.Comparison {
			t.Errorf("compare(%q, %q) = %d (valid %v), want %d", fixture.Left, fixture.Right, comparison, valid, *fixture.Comparison)
		}
	}
}
