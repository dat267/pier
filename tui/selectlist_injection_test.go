package tui

import (
	"strings"
	"testing"
)

// TestSelectListSanitizesItemText pins the list against terminal control
// injection from its items (D451). Reference: pi v1.5.0 f9a26365
// components/select-list.ts renders `item.label || item.value` verbatim, while
// the port sanitizes untrusted terminal text (D342, D422).
//
// The reachable path is @-completion: a cloned repository can contain a file
// whose name carries escape sequences (Linux permits it), and the suggestion
// list draws it while the user types.
func TestSelectListSanitizesItemText(t *testing.T) {
	payload := "@\x1b[2J\x1b]52;c;cGF3bmVk\x07evil.go"
	list := NewSelectList([]SelectItem{{
		Value: payload, Label: payload, Description: payload,
	}}, 5, plainSelectTheme(), SelectListLayoutOptions{})
	for _, line := range list.Render(100) {
		if strings.Contains(line, "\x1b[2J") || strings.Contains(line, "\x1b]52") {
			t.Fatalf("the list rendered item text raw: %q", line)
		}
	}
	// The value a caller inserts must stay exact: only the display is sanitized.
	if got := list.items[0].Value; got != payload {
		t.Fatalf("item value was rewritten: %q", got)
	}
}
