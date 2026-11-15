package tui

import (
	"encoding/json"
	"os"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) tui/src/components/markdown.ts. The corpus in
// testdata/mdrender-fuzz is produced by rendering seeded documents that mix
// markdown constructs with the reference's own Markdown component, so the port's
// renderer is compared against the reference on shapes the curated corpus in
// md_render_golden.txt does not spell out. The document text travels in the
// fixture, so the random generator is not duplicated here.
func TestMarkdownFuzzCorpusMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/mdrender-fuzz/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Label string   `json:"label"`
		Text  string   `json:"text"`
		Width int      `json:"width"`
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("the markdown fuzz corpus is empty")
	}
	theme := mdTestTheme()
	for _, fixture := range fixtures {
		markdown := NewMarkdown(fixture.Text, 0, 0, theme, nil, MarkdownOptions{})
		got := markdown.Render(fixture.Width)
		if len(got) != len(fixture.Lines) {
			t.Fatalf("%s (width %d) rendered %d lines, want %d\ninput %q\n got %q\nwant %q",
				fixture.Label, fixture.Width, len(got), len(fixture.Lines), fixture.Text, got, fixture.Lines)
		}
		for index := range got {
			if got[index] != fixture.Lines[index] {
				t.Fatalf("%s (width %d) line %d\ninput %q\n got %q\nwant %q",
					fixture.Label, fixture.Width, index, fixture.Text, got[index], fixture.Lines[index])
			}
		}
	}
}
