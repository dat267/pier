package tui

import "testing"

// Pinned pi v1.4.0 packages/tui/src/components/box.ts removes Box children
// with Array.splice and replaces the render mouse layout on the next successful
// render.
func TestBoxRemoveChildReleasesRenderedReferences(t *testing.T) {
	first := &changingFlatLines{lines: []string{"first component"}}
	removed := &changingFlatLines{lines: []string{"removed component"}}
	box := NewBox(0, 0, nil)
	box.AddChild(first)
	box.AddChild(removed)
	box.Render(80)

	box.RemoveChild(removed)
	box.Render(80)

	if refs := box.Children[:cap(box.Children)]; refs[1] != nil {
		t.Fatal("children backing slice retains removed component")
	}
	if refs := box.childLines[:cap(box.childLines)]; refs[1] != nil {
		t.Fatal("render scratch retains removed component lines")
	}
	if refs := box.mouseLayout[:cap(box.mouseLayout)]; refs[1].component != nil {
		t.Fatal("mouse layout retains removed component")
	}
}
