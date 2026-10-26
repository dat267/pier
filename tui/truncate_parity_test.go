package tui

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252), tui/src/utils.ts truncateToWidth.
// Regenerate:
//
//	node --experimental-strip-types tui/testdata/truncate/generate.mjs ./pi > tui/testdata/truncate/golden.json
func TestTruncateToWidthMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/truncate/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Text     string `json:"text"`
		MaxWidth int    `json:"maxWidth"`
		Ellipsis string `json:"ellipsis"`
		Pad      bool   `json:"pad"`
		Expected string `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty truncate corpus")
	}
	for _, fixture := range fixtures {
		got := TruncateToWidth(fixture.Text, fixture.MaxWidth, fixture.Ellipsis, fixture.Pad)
		if got != fixture.Expected {
			t.Fatalf("TruncateToWidth(%q, %d, %q, %t) = %q, want %q",
				fixture.Text, fixture.MaxWidth, fixture.Ellipsis, fixture.Pad, got, fixture.Expected)
		}
	}
}
