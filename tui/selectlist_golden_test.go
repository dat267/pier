package tui

// Replays the upstream select-list goldens (tui/testdata/select-list/
// generate.mjs), which hold the same item sets, layouts, widths and selection
// positions this file builds. Every picker renders through SelectList.

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func selectListGoldenSets() map[string][]SelectItem {
	item := func(label, description string) SelectItem {
		return SelectItem{Value: label, Label: label, Description: description}
	}
	return map[string][]SelectItem{
		"empty": {},
		"one":   {item("only", "")},
		"five":  {item("alpha", ""), item("beta", ""), item("gamma", ""), item("delta", ""), item("epsilon", "")},
		"descriptions": {
			item("read", "Read a file from disk"),
			item("bash", "Run a shell command"),
			item("edit", "Replace text in a file"),
		},
		"mixed": {
			item("short", "d"),
			item("a much longer label than the column", "a long description that has to be cut somewhere"),
			item("日本語のラベル", "説明文"),
			item("😀 emoji", "wide chars"),
			item("", "empty label"),
			item("valueonly", ""),
		},
		"many": func() []SelectItem {
			items := make([]SelectItem, 0, 12)
			for index := 1; index <= 12; index++ {
				items = append(items, item(fmt.Sprintf("item %d", index), fmt.Sprintf("description %d", index)))
			}
			return items
		}(),
		"wide": {item("日本語日本語日本語日本語", "説明"), item("x", "y")},
	}
}

func selectListGoldenLayouts() []SelectListLayoutOptions {
	return []SelectListLayoutOptions{
		{},
		{MinPrimaryColumnWidth: 10, HasMin: true, MaxPrimaryColumnWidth: 20, HasMax: true},
		{MaxPrimaryColumnWidth: 8, HasMax: true},
		{MinPrimaryColumnWidth: 40, HasMin: true},
	}
}

func TestSelectListRenderingMatchesTheReference(t *testing.T) {
	data, err := os.ReadFile("testdata/select-list/golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var records []struct {
		Set        string   `json:"set"`
		Layout     int      `json:"layout"`
		Width      int      `json:"width"`
		MaxVisible int      `json:"maxVisible"`
		Selected   int      `json:"selected"`
		Lines      []string `json:"lines"`
	}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("golden is empty")
	}
	sets := selectListGoldenSets()
	layouts := selectListGoldenLayouts()
	identity := func(text string) string { return text }
	theme := SelectListTheme{
		SelectedPrefix: identity,
		SelectedText:   identity,
		Description:    identity,
		ScrollInfo:     identity,
		NoMatch:        identity,
	}
	for _, record := range records {
		items, ok := sets[record.Set]
		if !ok {
			t.Fatalf("golden names unknown item set %q", record.Set)
		}
		if record.Layout < 0 || record.Layout >= len(layouts) {
			t.Fatalf("golden names unknown layout %d", record.Layout)
		}
		list := NewSelectList(items, record.MaxVisible, theme, layouts[record.Layout])
		list.SetSelectedIndex(record.Selected)
		got := list.Render(record.Width)
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
