package tui

import (
	"strings"
	"testing"
)

// Pinned pi v1.4.0 packages/tui/src/components/box.ts renders its live child
// tree on each pass. Box mutations must invalidate the Go ancestor's
// SkipUnchangedChildren frame as well.
func TestBoxMutationsInvalidateWarmContainerFrame(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*Box)
		mutate func(*Box)
		check  func(*testing.T, []string)
	}{
		{
			name:   "add child",
			setup:  func(box *Box) { box.AddChild(NewText("first", 0, 0, nil)) },
			mutate: func(box *Box) { box.AddChild(NewText("second", 0, 0, nil)) },
			check: func(t *testing.T, lines []string) {
				if !hasLine(lines, "second") {
					t.Fatalf("render after adding child = %q", lines)
				}
			},
		},
		{
			name: "remove child",
			setup: func(box *Box) {
				box.AddChild(NewText("first", 0, 0, nil))
				box.AddChild(NewText("removed", 0, 0, nil))
			},
			mutate: func(box *Box) { box.RemoveChild(box.Children[1]) },
			check: func(t *testing.T, lines []string) {
				if hasLine(lines, "removed") {
					t.Fatalf("render after removing child = %q", lines)
				}
			},
		},
		{
			name:   "clear",
			setup:  func(box *Box) { box.AddChild(NewText("first", 0, 0, nil)) },
			mutate: func(box *Box) { box.Clear() },
			check: func(t *testing.T, lines []string) {
				if len(lines) != 0 {
					t.Fatalf("render after clear = %q", lines)
				}
			},
		},
		{
			name:   "padding",
			setup:  func(box *Box) { box.AddChild(NewText("x", 0, 0, nil)) },
			mutate: func(box *Box) { box.SetPaddingX(2) },
			check: func(t *testing.T, lines []string) {
				if len(lines) == 0 || !strings.HasPrefix(lines[0], "  x") {
					t.Fatalf("render after padding change = %q", lines)
				}
			},
		},
		{
			name:   "background",
			setup:  func(box *Box) { box.AddChild(NewText("x", 0, 0, nil)) },
			mutate: func(box *Box) { box.SetBgFn(func(line string) string { return "<" + line + ">" }) },
			check: func(t *testing.T, lines []string) {
				if len(lines) == 0 || !strings.Contains(lines[0], "<x") {
					t.Fatalf("render after background change = %q", lines)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			box := NewBox(0, 0, nil)
			tc.setup(box)
			chat := &Container{SkipUnchangedChildren: true}
			chat.AddChild(box)
			chat.Render(12)

			tc.mutate(box)
			tc.check(t, chat.Render(12))
		})
	}
}
