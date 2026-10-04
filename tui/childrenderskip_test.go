package tui

import (
	"strings"
	"testing"
)

// countingVersioned is a versioned child that counts its Render calls.
type countingVersioned struct {
	*Container
	renders int
}

func (c *countingVersioned) Render(width int) []string {
	c.renders++
	return c.Container.Render(width)
}

func hasLine(lines []string, want string) bool {
	for _, line := range lines {
		if strings.TrimRight(line, " ") == want {
			return true
		}
	}
	return false
}

// TestContainerSkipsUnchangedVersionedChildren pins the scroll fix: a
// SkipUnchangedChildren container does not call a child's Render while the
// child's revision is unchanged, and re-renders it once the revision bumps.
func TestContainerSkipsUnchangedVersionedChildren(t *testing.T) {
	chat := &Container{SkipUnchangedChildren: true}
	child := &countingVersioned{Container: &Container{}}
	child.Container.AddChild(NewText("hello", 0, 0, nil))
	chat.AddChild(child)

	if lines := chat.Render(40); !hasLine(lines, "hello") {
		t.Fatalf("first render = %q", lines)
	}
	if child.renders != 1 {
		t.Fatalf("renders = %d, want 1", child.renders)
	}
	chat.Render(40)
	chat.Render(40)
	if child.renders != 1 {
		t.Fatalf("unchanged child re-rendered: %d", child.renders)
	}

	// A revision change re-renders it and the new content reaches the parent.
	child.Container.Clear()
	child.Container.AddChild(NewText("world", 0, 0, nil))
	child.MarkDirty()
	if lines := chat.Render(40); !hasLine(lines, "world") {
		t.Fatalf("changed child lines = %q", lines)
	}
	if child.renders != 2 {
		t.Fatalf("renders = %d after a revision bump, want 2", child.renders)
	}
}

// TestContainerSkipTracksChildIdentityNotJustRevision guards the reuse path: a
// fresh child can share a revision (both start at 0), so the skip must match
// the component, not only the counter.
func TestContainerSkipTracksChildIdentityNotJustRevision(t *testing.T) {
	chat := &Container{SkipUnchangedChildren: true}
	first := &countingVersioned{Container: &Container{}}
	first.Container.AddChild(NewText("first", 0, 0, nil))
	chat.AddChild(first)
	if lines := chat.Render(40); !hasLine(lines, "first") {
		t.Fatalf("first render = %q", lines)
	}

	// Replace the child in place without dropping the parent's cache: a fresh
	// child at the same index shares revision 0.
	second := &countingVersioned{Container: &Container{}}
	second.Container.AddChild(NewText("second", 0, 0, nil))
	chat.Children[0] = second
	if lines := chat.Render(40); !hasLine(lines, "second") {
		t.Fatalf("stale lines served for a replaced child: %q", lines)
	}
}

// TestContainerWithoutSkipRendersChildrenEveryFrame pins the default.
func TestContainerWithoutSkipRendersChildrenEveryFrame(t *testing.T) {
	chat := &Container{}
	child := &countingVersioned{Container: &Container{}}
	child.Container.AddChild(NewText("hello", 0, 0, nil))
	chat.AddChild(child)
	chat.Render(40)
	chat.Render(40)
	if child.renders != 2 {
		t.Fatalf("renders = %d, want 2 without the skip", child.renders)
	}
}

func benchSkipChat(tools, outputLines int, skip bool) *Container {
	chat := &Container{SkipUnchangedChildren: skip}
	var sb strings.Builder
	for i := 0; i < outputLines; i++ {
		sb.WriteString("some tool output line with several words to wrap around the terminal width\n")
	}
	output := sb.String()
	for i := 0; i < tools; i++ {
		child := &countingVersioned{Container: &Container{}}
		box := NewBox(2, 1, func(value string) string { return "\x1b[48;2;1;1;1m" + value + "\x1b[0m" })
		box.AddChild(NewMouseRegion(NewText(output, 0, 0, nil), nil))
		child.Container.AddChild(box)
		chat.AddChild(child)
	}
	return chat
}

func BenchmarkChatWarmRender(b *testing.B) {
	for _, skip := range []bool{false, true} {
		name := "walk"
		if skip {
			name = "skip"
		}
		b.Run(name, func(b *testing.B) {
			chat := benchSkipChat(500, 40, skip)
			chat.Render(120)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				chat.Render(120)
			}
		})
	}
}

// TestContainerBumpRevisionKeepsTheChildCache pins the narrow tick's signal:
// BumpRevision makes the parent re-render the child, but the child's own cache
// survives, so Render reuses its cached lines instead of re-flattening.
func TestContainerBumpRevisionKeepsTheChildCache(t *testing.T) {
	chat := &Container{SkipUnchangedChildren: true}
	child := &countingVersioned{Container: &Container{}}
	child.Container.AddChild(NewText("hello", 0, 0, nil))
	chat.AddChild(child)
	if lines := chat.Render(40); !hasLine(lines, "hello") {
		t.Fatalf("first render = %q", lines)
	}
	if child.renders != 1 {
		t.Fatalf("renders = %d, want 1", child.renders)
	}
	// The tick bumps only the revision; the parent re-renders the child.
	child.BumpRevision()
	if lines := chat.Render(40); !hasLine(lines, "hello") {
		t.Fatalf("render after BumpRevision = %q", lines)
	}
	if child.renders != 2 {
		t.Fatalf("renders = %d after BumpRevision, want 2", child.renders)
	}
}
