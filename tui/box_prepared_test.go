package tui

import (
	"reflect"
	"testing"
)

func TestBoxPreparedFrameRejectsWrongGeometryAndBackground(t *testing.T) {
	box := NewBox(1, 1, func(text string) string { return "old:" + text })
	box.AddChild(NewText("x", 0, 0, nil))
	before := box.Render(8)
	for _, prepared := range []*Box{NewBox(2, 1, func(text string) string { return "old:" + text }), NewBox(1, 2, func(text string) string { return "old:" + text }), NewBox(1, 1, func(text string) string { return "new:" + text })} {
		prepared.AddChild(NewText("x", 0, 0, nil))
		prepared.Render(8)
		if box.AdoptPreparedFrame(prepared) {
			t.Fatal("wrong prepared frame accepted")
		}
		if !reflect.DeepEqual(box.Render(8), before) {
			t.Fatal("rejected frame changed attached display")
		}
	}
}

func TestBoxPreparedFramePreservesOwnerMouseTargets(t *testing.T) {
	hit := &mouseRecordingComponent{staticComponent: staticComponent{lines: []string{"x", "y"}}}
	box := NewBox(1, 1, nil)
	box.AddChild(hit)
	prepared := NewBox(1, 1, nil)
	prepared.AddChild(&staticComponent{lines: []string{"x", "y"}})
	prepared.Render(8)
	if !box.AdoptPreparedFrame(prepared) {
		t.Fatal("prepared frame rejected")
	}
	event := TuiMouseEvent{Type: MousePress, Button: MouseButtonLeft, X: 1, Y: 2, Width: 8, Height: 4}
	result := box.HandleMouse(event)
	if result == nil || result.Target.Component != hit || hit.last.X != 0 || hit.last.Y != 1 {
		t.Fatal("handoff replaced owner target or mouse geometry")
	}
}

// D206: packages/tui/src/components/box.ts applies padding synchronously.
// A caller with an immutable, generation-checked snapshot can hand its frame
// to the owner without reapplying the background to every result line.
func TestBoxAdoptsPreparedFrameWithoutRepainting(t *testing.T) {
	calls := 0
	bg := func(text string) string { calls++; return "[" + text + "]" }
	box := NewBox(1, 1, bg)
	child := NewText("x", 0, 0, nil)
	box.AddChild(child)
	adopter, ok := any(box).(interface{ AdoptPreparedFrame(*Box) bool })
	if !ok {
		t.Fatal("Box has no owner-applied prepared frame handoff")
	}
	prepared := NewBox(1, 1, bg)
	prepared.AddChild(NewText("x", 0, 0, nil))
	prepared.Render(8)
	if !adopter.AdoptPreparedFrame(prepared) {
		t.Fatal("matching prepared frame rejected")
	}
	calls = 0
	got := box.Render(8)
	if calls != 0 {
		t.Fatalf("owner repainted %d lines after frame handoff", calls)
	}
	expected := []string{"[        ]", "[ x      ]", "[        ]"}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("frame=%q, want %q", got, expected)
	}
	child.SetText("y")
	if reflect.DeepEqual(box.Render(8), expected) {
		t.Fatal("prepared cache hid later owner changes")
	}
}
