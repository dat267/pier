package tui

import (
	"fmt"
	"testing"
)

// benchmarkLayoutTree builds a representative conversation view: a scroll view
// over many text lines in a container.
func benchmarkLayoutTree(lines int) Component {
	content := &Container{}
	for index := 0; index < lines; index++ {
		content.AddChild(NewText(fmt.Sprintf("message line %d with some words to wrap", index), 0, 0, nil))
	}
	return NewScrollView(content, ScrollViewOptions{})
}

// BenchmarkRenderLayoutFrameScrollView measures one full layout pass (measure
// and paint) over a conversation-sized view, the cost paid on every requested
// frame.
func BenchmarkRenderLayoutFrameScrollView(b *testing.B) {
	root := benchmarkLayoutTree(500)
	_ = RenderLayoutFrame(root, 100, 40, func() {})
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		_ = RenderLayoutFrame(root, 100, 40, func() {})
	}
}

func BenchmarkRenderLayoutFrameNestedBoxes(b *testing.B) {
	root := NewBox(0, 0, nil)
	for index := 0; index < 50; index++ {
		child := NewBox(0, 0, nil)
		child.AddChild(NewText(fmt.Sprintf("row %d", index), 0, 0, nil))
		root.AddChild(child)
	}
	_ = RenderLayoutFrame(root, 80, 40, func() {})
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		_ = RenderLayoutFrame(root, 80, 40, func() {})
	}
}
