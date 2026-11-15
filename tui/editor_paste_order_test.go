package tui

import "testing"

// Upstream expands pastes by iterating its paste Map, whose order is insertion
// order; Go's map order is random. The order matters because a paste body can
// itself carry a later paste's marker: expanding the higher ID first leaves
// that marker sitting in the text while its replacement is a no-op.
func TestPasteExpansionOrderIsInsertionOrder(t *testing.T) {
	editor := NewEditor(nil, EditorTheme{}, EditorOptions{})
	editor.pastes[3] = "third"
	editor.pastes[1] = "first contains [paste #2 6 chars]"
	editor.pastes[2] = "second"
	editor.pasteCounter = 3

	got := editor.pasteExpansionOrder()
	want := []int{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}
