package tui

// Replays the upstream text/box goldens (tui/testdata/box/generate.mjs): the
// same text, paddings, background and width this file builds, rendered once as a
// Text and once as a Box around an unpadded Text. A panel is that composition.

import (
	"encoding/json"
	"os"
	"testing"
)

func boxGoldenTexts() map[string]string {
	return map[string]string{
		"empty":          "",
		"short":          "hello",
		"words":          "one two three four five six seven eight nine ten",
		"longWord":       "supercalifragilisticexpialidocious",
		"multiline":      "first line\nsecond line is longer\nthird",
		"trailingSpaces": "trailing   \nspaces   ",
		"wide":           "日本語のテキスト です",
		"emoji":          "😀 emoji 😀 text",
		"styled":         "\x1b[31mred\x1b[0m and \x1b[1mbold\x1b[0m",
		"indented":       "  two leading spaces",
	}
}

func TestTextAndBoxRenderingMatchTheReference(t *testing.T) {
	data, err := os.ReadFile("testdata/box/golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var records []struct {
		Component string   `json:"component"`
		Text      string   `json:"text"`
		PaddingX  int      `json:"paddingX"`
		PaddingY  int      `json:"paddingY"`
		BG        string   `json:"bg"`
		Width     int      `json:"width"`
		Lines     []string `json:"lines"`
	}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("golden is empty")
	}
	texts := boxGoldenTexts()
	background := func(name string) func(string) string {
		switch name {
		case "none":
			return nil
		case "marker":
			return func(text string) string { return "[" + text + "]" }
		default:
			t.Fatalf("golden names unknown background %q", name)
			return nil
		}
	}
	for _, record := range records {
		text, ok := texts[record.Text]
		if !ok {
			t.Fatalf("golden names unknown text %q", record.Text)
		}
		bg := background(record.BG)
		var got []string
		switch record.Component {
		case "text":
			got = NewText(text, record.PaddingX, record.PaddingY, bg).Render(record.Width)
		case "box":
			box := NewBox(record.PaddingX, record.PaddingY, bg)
			box.AddChild(NewText(text, 0, 0, nil))
			got = box.Render(record.Width)
		default:
			t.Fatalf("golden names unknown component %q", record.Component)
		}
		if len(got) != len(record.Lines) {
			t.Fatalf("%+v: %d lines, want %d\ngot %q\nwant %q", record, len(got), len(record.Lines), got, record.Lines)
		}
		for index := range got {
			if got[index] != record.Lines[index] {
				t.Errorf("%+v line %d:\ngot  %q\nwant %q", record, index, got[index], record.Lines[index])
			}
		}
	}
}
