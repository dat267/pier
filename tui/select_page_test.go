package tui

// pi v1.5.0 keybindings.ts getSelectPageIndex and the page-key cases in
// select-list.ts/settings-list.ts/editor.ts: PageUp/PageDown move a list
// selection by one visible page, clamped to the list bounds, and an open
// autocomplete menu receives the keys.

import "testing"

func useDefaultKeybindings(t *testing.T) {
	t.Helper()
	previous := GetKeybindings()
	SetKeybindings(NewKeybindingsManager(TUIKeybindings, nil))
	t.Cleanup(func() { SetKeybindings(previous) })
}

func TestSelectListPageKeys(t *testing.T) {
	useDefaultKeybindings(t)
	items := make([]SelectItem, 30)
	for i := range items {
		items[i] = SelectItem{Value: string(rune('a' + i%26)), Label: "item"}
	}
	list := NewSelectList(items, 5, SelectListTheme{}, SelectListLayoutOptions{})
	list.HandleInput("\x1b[6~") // PageDown
	if list.selectedIndex != 5 {
		t.Fatalf("page down moved to %d, want 5", list.selectedIndex)
	}
	list.HandleInput("\x1b[5~") // PageUp
	if list.selectedIndex != 0 {
		t.Fatalf("page up moved to %d, want 0", list.selectedIndex)
	}
	// Clamped at the end.
	list.selectedIndex = 28
	list.HandleInput("\x1b[6~")
	if list.selectedIndex != 29 {
		t.Fatalf("page down clamped to %d, want 29", list.selectedIndex)
	}
	list.HandleInput("\x1b[6~")
	if list.selectedIndex != 29 {
		t.Fatalf("page down past the end moved to %d, want 29", list.selectedIndex)
	}
}

func TestSelectPageIndexEmptyList(t *testing.T) {
	kb := NewKeybindingsManager(TUIKeybindings, nil)
	if index, ok := SelectPageIndex(kb, "\x1b[6~", 3, 0, 5); !ok || index != 0 {
		t.Fatalf("empty list page = %d,%v", index, ok)
	}
	if _, ok := SelectPageIndex(kb, "x", 3, 10, 5); ok {
		t.Fatal("a plain key reported a page")
	}
}

func TestSettingsListPageKeys(t *testing.T) {
	useDefaultKeybindings(t)
	items := make([]SettingItem, 20)
	for i := range items {
		items[i] = SettingItem{ID: "id", Label: "label", CurrentValue: "value", Values: []string{"value"}}
	}
	list := NewSettingsList(items, 4, SettingsListTheme{}, func(string, string) {}, func() {}, SettingsListOptions{})
	list.HandleInput("\x1b[6~")
	if list.selectedIndex != 4 {
		t.Fatalf("page down moved to %d, want 4", list.selectedIndex)
	}
	list.HandleInput("\x1b[5~")
	if list.selectedIndex != 0 {
		t.Fatalf("page up moved to %d, want 0", list.selectedIndex)
	}
}
