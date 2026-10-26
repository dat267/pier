package tui

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252), tui/src/utils.ts wrapTextWithAnsi.
// Regenerate:
//
//	node --experimental-strip-types tui/testdata/wrap/generate.mjs ./pi > tui/testdata/wrap/golden.json
func TestWrapTextWithAnsiMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/wrap/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Text     string   `json:"text"`
		Width    int      `json:"width"`
		Expected []string `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty wrap corpus")
	}
	for _, fixture := range fixtures {
		got := WrapTextWithAnsi(fixture.Text, fixture.Width)
		if len(got) != len(fixture.Expected) {
			t.Fatalf("WrapTextWithAnsi(%q, %d) = %q, want %q", fixture.Text, fixture.Width, got, fixture.Expected)
		}
		for index := range got {
			if got[index] != fixture.Expected[index] {
				t.Fatalf("WrapTextWithAnsi(%q, %d)[%d] = %q, want %q", fixture.Text, fixture.Width, index, got[index], fixture.Expected[index])
			}
		}
	}
}
