package interactive

import (
	"testing"

	"github.com/dat267/pier/tui"
)

// Reference: pi v1.5.2 modes/interactive/components/settings-submenu.ts
// handleInput treats tui.select.pageUp and tui.select.pageDown as navigation
// keys of a searchable submenu, so a page key moves the list instead of
// reaching the filter input.
func TestSearchableSubmenuPagesWithoutTypingIntoTheQuery(t *testing.T) {
	InitTheme("dark", false)
	options := make([]tui.SelectItem, 12)
	for index := range options {
		options[index] = tui.SelectItem{Value: itoa(index), Label: "Item " + itoa(index)}
	}
	submenu := NewSelectSubmenu("Title", "description", options, "", nil, nil, nil,
		SelectSubmenuOptions{Searchable: true})

	submenu.HandleInput("\x1b[6~") // pageDown
	if index := submenu.GetSelectList().SelectedIndex(); index != 10 {
		t.Fatalf("PageDown selected %d, want the second page start 10", index)
	}
	if value := submenu.searchInput.Value(); value != "" {
		t.Fatalf("query = %q, want the page key kept out of the filter", value)
	}
	submenu.HandleInput("\x1b[5~") // pageUp
	if index := submenu.GetSelectList().SelectedIndex(); index != 0 {
		t.Fatalf("PageUp selected %d, want 0", index)
	}
}
