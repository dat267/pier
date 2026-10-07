package tui

import "testing"

type changingFlatLines struct {
	lines   []string
	version uint64
}

func (c *changingFlatLines) Render(int) []string           { return c.lines }
func (c *changingFlatLines) Invalidate()                   { c.version++ }
func (c *changingFlatLines) RenderVersion() (uint64, bool) { return c.version, true }
func (c *changingFlatLines) change() {
	c.version++
	if c.version%2 == 0 {
		c.lines[0] = "even"
	} else {
		c.lines[0] = "odd"
	}
}

// D209: packages/tui/src/tui.ts allocates a fresh flattened Container frame.
// A changed first child must reuse owner storage just as a changed tail does.
func TestContainerFirstChildChangeReusesStorage(t *testing.T) {
	child := &changingFlatLines{lines: make([]string, 200)}
	for i := range child.lines {
		child.lines[i] = "line"
	}
	container := &Container{}
	container.AddChild(child)
	before := container.Render(80)
	child.change()
	after := container.Render(80)
	if &before[0] != &after[0] {
		t.Fatal("first-child change allocated a new flattened frame")
	}
	allocs := testing.AllocsPerRun(10, func() { child.change(); container.Render(80) })
	if allocs != 0 {
		t.Fatalf("first-child changes allocated %.0f times, want zero", allocs)
	}
	if after[0] != "even" && after[0] != "odd" {
		t.Fatal("reused frame missed changed output")
	}
}

func TestContainerShrinkClearsRemovedLineReferences(t *testing.T) {
	child := &changingFlatLines{lines: []string{"keep", "removed one", "removed two"}}
	container := &Container{}
	container.AddChild(child)
	before := container.Render(80)
	child.lines = child.lines[:1]
	child.Invalidate()
	after := container.Render(80)
	if len(after) != 1 || after[0] != "keep" {
		t.Fatal("shrink returned wrong frame")
	}
	if &before[0] != &after[0] {
		t.Fatal("shrink discarded reusable owner storage")
	}
	for _, line := range after[:cap(after)][1:] {
		if line != "" {
			t.Fatal("reused storage retained removed line references")
		}
	}
}

func BenchmarkContainerFirstChildChange(b *testing.B) {
	child := &changingFlatLines{lines: make([]string, 30000)}
	for i := range child.lines {
		child.lines[i] = "plain output with words and spaces"
	}
	container := &Container{}
	container.AddChild(child)
	container.Render(80)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child.change()
		container.Render(80)
	}
}
