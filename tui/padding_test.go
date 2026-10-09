package tui

import (
	"strings"
	"testing"
)

// SetPaddingX changes the horizontal padding in place, which is what lets the tool content box
// follow the outputPad setting without being rebuilt (upstream setPaddingX, #10557).
func TestBoxSetPaddingXAppliesWithoutRebuilding(t *testing.T) {
	box := NewBox(0, 0, nil)
	box.AddChild(NewText("hello", 0, 0, nil))
	if got := box.Render(20); len(got) == 0 || strings.HasPrefix(got[0], " ") {
		t.Fatalf("unpadded box rendered %q", got)
	}
	box.SetPaddingX(2)
	padded := box.Render(20)
	if len(padded) == 0 || !strings.HasPrefix(padded[0], "  ") || !strings.Contains(padded[0], "hello") {
		t.Fatalf("padded box rendered %q", padded)
	}
	// Setting the same value again is a no-op, and setting it back restores the first render.
	box.SetPaddingX(2)
	box.SetPaddingX(0)
	restored := box.Render(20)
	if len(restored) != len(padded) || strings.HasPrefix(restored[0], " ") {
		t.Fatalf("padding was not restored: %q", restored)
	}
}

func TestTextSetPaddingXAppliesWithoutRebuilding(t *testing.T) {
	text := NewText("hello", 1, 0, nil)
	padded := text.Render(20)
	if len(padded) == 0 || !strings.HasPrefix(padded[0], " ") {
		t.Fatalf("text rendered %q", padded)
	}
	text.SetPaddingX(0)
	unpadded := text.Render(20)
	if len(unpadded) == 0 || strings.HasPrefix(unpadded[0], " ") {
		t.Fatalf("unpadded text rendered %q", unpadded)
	}
}
