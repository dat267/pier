package interactive

import (
	"testing"

	"github.com/dat267/pier/tui"
)

// ---- Chat viewport ----

// TestChatViewportEditorMinSize pins upstream's editor dock entry
// (minSize: 3): the editor dock row keeps top border + one input line +
// bottom border under squeeze, instead of pier's default minSize 0.
func TestChatViewportEditorMinSize(t *testing.T) {
	document := &staticLines{lines: []string{"doc"}}
	pending := &staticLines{lines: []string{"pending"}}
	status := &staticLines{lines: []string{"status"}}
	editor := &staticLines{lines: []string{"editor"}}
	footer := &staticLines{lines: []string{"footer"}}

	viewport := CreateChatViewport(ChatViewportOptions{
		Document: document, PendingMessages: pending, Status: status, Editor: editor, Footer: footer,
		Scrollbar: tui.ScrollbarHidden,
	})
	root, ok := viewport.Root.(*tui.VStack)
	if !ok {
		t.Fatalf("root is %T, want *tui.VStack", viewport.Root)
	}
	if len(root.Entries) != 2 {
		t.Fatalf("root entries = %d, want 2 (transcript + dock)", len(root.Entries))
	}
	dock, ok := root.Entries[1].Component.(*tui.VStack)
	if !ok {
		t.Fatalf("dock is %T, want *tui.VStack", root.Entries[1].Component)
	}
	for _, entry := range dock.Entries {
		if entry.Component == editor {
			if entry.MinSize != 3 {
				t.Fatalf("editor dock entry minSize = %d, want 3 (upstream parity)", entry.MinSize)
			}
			return
		}
	}
	t.Fatal("editor entry not found in the dock")
}

// ---- Model search ----

// TestModelSearchAgainstUpstreamGolden verifies the search text builders.
func TestModelSearchAgainstUpstreamGolden(t *testing.T) {
	golden := loadArrowGolden(t, "testdata/modelsearch_golden.txt", "")

	items := []ModelSearchItem{
		{ID: "gpt-5", Provider: "openai"},
		{ID: "gpt-5", Provider: "openai", Name: "GPT-5", HasName: true},
		{ID: "gpt-5", Provider: "openrouter/openai", Name: "OR", HasName: true},
		{ID: "", Provider: "p"},
	}
	for _, item := range items {
		key := `{"id":"` + item.ID + `","provider":"` + item.Provider + `"`
		if item.HasName {
			key += `,"name":"` + item.Name + `"`
		}
		key += `}`
		searchKey := "search " + key
		selectorKey := "selector " + key
		if want, ok := golden[searchKey]; ok {
			if got := mustJSON(GetModelSearchText(item)); got != want {
				t.Fatalf("%s: got %s want %s", searchKey, got, want)
			}
		}
		if want, ok := golden[selectorKey]; ok {
			if got := mustJSON(GetModelSelectorSearchText(item)); got != want {
				t.Fatalf("%s: got %s want %s", selectorKey, got, want)
			}
		}
	}
}

// ---- External editor ----
