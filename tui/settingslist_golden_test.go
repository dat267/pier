package tui

// Replays the upstream settings-list goldens (tui/testdata/settings-list/
// generate.mjs); the generator holds the same item sets this file builds and
// selects the same item in each list. Search is off, so both sides render the
// same rows.

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func settingsListGoldenSets() map[string][]SettingItem {
	item := func(id, label, value, description string) SettingItem {
		return SettingItem{ID: id, Label: label, CurrentValue: value, Description: description}
	}
	return map[string][]SettingItem{
		"empty": {},
		"one":   {item("theme", "Theme", "dark", "")},
		"five": {
			item("theme", "Theme", "dark", ""),
			item("model", "Model", "claude-sonnet-4-5", ""),
			item("steering", "Steering mode", "one-at-a-time", ""),
			item("compaction", "Auto-compaction", "on", ""),
			item("retry", "Auto-retry", "on", ""),
		},
		"descriptions": {
			item("theme", "Theme", "dark", "The colour theme used by the interface and the exported HTML."),
			item("model", "Model", "stub/m", "The model new turns use. Existing messages keep their model."),
			item("images", "Images", "off", "Short one."),
		},
		"long": {
			item("a", "A very long label that exceeds the alignment cap of thirty-six cells for sure", "value", ""),
			item("b", "Short", "a value that is much longer than the space left for the value column", ""),
			item("c", "日本語のラベル", "値", ""),
			item("d", "😀 emoji label", "emoji value 😀", ""),
		},
		"many": func() []SettingItem {
			items := make([]SettingItem, 0, 12)
			for index := 1; index <= 12; index++ {
				items = append(items, item(fmt.Sprintf("id%d", index-1), fmt.Sprintf("Setting %d", index), fmt.Sprintf("value %d", index), ""))
			}
			return items
		}(),
	}
}

func TestSettingsListRenderingMatchesTheReference(t *testing.T) {
	data, err := os.ReadFile("testdata/settings-list/golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var records []struct {
		Set        string   `json:"set"`
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
	sets := settingsListGoldenSets()
	identity := func(text string) string { return text }
	theme := SettingsListTheme{
		Label:       func(text string, selected bool) string { return text },
		Value:       func(text string, selected bool) string { return text },
		Description: identity,
		Cursor:      "→ ",
		Hint:        identity,
	}
	for _, record := range records {
		items, ok := sets[record.Set]
		if !ok {
			t.Fatalf("golden names unknown item set %q", record.Set)
		}
		list := NewSettingsList(items, record.MaxVisible, theme, func(string, string) {}, func() {}, SettingsListOptions{})
		if record.Selected >= 0 && record.Selected < len(items) {
			list.SelectItem(items[record.Selected].ID)
		}
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
