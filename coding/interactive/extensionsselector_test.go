package interactive

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var selectorEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plainSelectorLines(lines []string) []string {
	plain := make([]string, len(lines))
	for index, line := range lines {
		plain[index] = selectorEscape.ReplaceAllString(line, "")
	}
	return plain
}

// pi v1.5.2 (b4afdddf), modes/interactive/components/extension-selector.ts
// updateList keeps at most 10 options visible, centers the window on the
// selection and clamps it to the list, then appends a muted (n/total) line when
// either end is cut off. The auth provider list is long enough to scroll.
func TestExtensionSelectorWindowsLongLists(t *testing.T) {
	newRendererTestTheme(t)
	options := make([]string, 25)
	for index := range options {
		options[index] = "option-" + strconv.Itoa(index+1)
	}
	selector := NewExtensionSelectorComponent("Pick", options, func(string) {}, func() {})
	defer selector.Dispose()

	renderList := func() []string { return plainSelectorLines(selector.listContainer.Render(80)) }
	top := renderList()
	if len(top) != 11 || !strings.Contains(top[0], "option-1 ") || !strings.Contains(top[9], "option-10 ") {
		t.Fatalf("top window = %q, want the 10 rows of options 1..10", top)
	}
	if !strings.Contains(top[10], "(1/25)") {
		t.Fatalf("top scroll line = %q", top[10])
	}
	if strings.Contains(strings.Join(top, "\n"), "option-11") {
		t.Fatalf("top window shows a hidden row: %q", top)
	}

	for range 20 {
		selector.HandleInput("j")
	}
	if selector.SelectedIndex() != 20 {
		t.Fatalf("selection = %d, want 20", selector.SelectedIndex())
	}
	bottom := renderList()
	if len(bottom) != 11 {
		t.Fatalf("bottom window = %q, want 10 rows and the scroll line", bottom)
	}
	if !strings.Contains(bottom[0], "option-16 ") || !strings.Contains(bottom[9], "option-25 ") {
		t.Fatalf("bottom window = %q, want options 16..25", bottom)
	}
	if !strings.Contains(bottom[10], "(21/25)") {
		t.Fatalf("scroll line = %q", bottom[10])
	}
	if strings.Contains(strings.Join(bottom, "\n"), "option-15") {
		t.Fatalf("bottom window kept a hidden row: %q", bottom)
	}
}

// Upstream confirms only a truthy option: an empty label never fires the
// selection callback.
func TestExtensionSelectorIgnoresEmptyOption(t *testing.T) {
	newRendererTestTheme(t)
	var selected []string
	selector := NewExtensionSelectorComponent("Pick", []string{"", "second"}, func(option string) {
		selected = append(selected, option)
	}, func() {})
	defer selector.Dispose()
	selector.HandleInput("\n")
	if len(selected) != 0 {
		t.Fatalf("empty option fired the callback: %q", selected)
	}
	selector.HandleInput("j")
	selector.HandleInput("\n")
	if len(selected) != 1 || selected[0] != "second" {
		t.Fatalf("selected = %q", selected)
	}
}
