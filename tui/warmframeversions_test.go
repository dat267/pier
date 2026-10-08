package tui

import "testing"

// versionCountingChild counts how many times a SkipUnchangedChildren parent
// asks for its render revision.
type versionCountingChild struct {
	*Container
	versionCalls int
}

func (c *versionCountingChild) RenderVersion() (uint64, bool) {
	c.versionCalls++
	return c.Container.RenderVersion()
}

// TestContainerReadsEachChildVersionOncePerWarmFrame pins the warm-frame scan
// cost: the change-detection walk asks a versioned child for its revision
// exactly once, reusing that read for both the reuse decision and the
// first-changed comparison. A second interface dispatch per child per frame is
// what the walk used to pay.
func TestContainerReadsEachChildVersionOncePerWarmFrame(t *testing.T) {
	chat := &Container{SkipUnchangedChildren: true}
	child := &versionCountingChild{Container: &Container{}}
	child.Container.AddChild(NewText("hello", 0, 0, nil))
	chat.AddChild(child)
	chat.Render(40)

	child.versionCalls = 0
	chat.Render(40)
	if child.versionCalls != 1 {
		t.Fatalf("a warm frame read the child version %d times, want 1", child.versionCalls)
	}
	chat.Render(40)
	if child.versionCalls != 2 {
		t.Fatalf("two warm frames read the child version %d times, want 1 per frame", child.versionCalls)
	}
}

// TestSpacerReportsAStableRevision lets a SkipUnchangedChildren parent reuse an
// unchanged spacer instead of calling Render for every transcript spacer on
// every paint. The revision must move when the spacer's content changes.
func TestSpacerReportsAStableRevision(t *testing.T) {
	spacer := NewSpacer(2)
	first, ok := spacer.RenderVersion()
	if !ok {
		t.Fatal("spacer does not report a render revision")
	}
	if again, _ := spacer.RenderVersion(); again != first {
		t.Fatalf("revision moved without a change: %d then %d", first, again)
	}

	spacer.SetLines(3)
	afterSet, _ := spacer.RenderVersion()
	if afterSet == first {
		t.Fatal("SetLines did not move the spacer revision")
	}

	spacer.Invalidate()
	afterInvalidate, _ := spacer.RenderVersion()
	if afterInvalidate == afterSet {
		t.Fatal("Invalidate did not move the spacer revision")
	}
}

// TestContainerWarmFrameSkipsTheChildWalk pins the fast path: a SkipUnchangedChildren
// container whose children are all reusable returns its cached frame without
// snapshotting the child list, rebuilding the mouse layout, or re-flattening.
func TestContainerWarmFrameSkipsTheChildWalk(t *testing.T) {
	chat := &Container{SkipUnchangedChildren: true}
	child := &versionCountingChild{Container: &Container{}}
	child.Container.AddChild(NewText("hello", 0, 0, nil))
	chat.AddChild(child)
	chat.Render(40) // cold frame walks
	warm := chat.RenderWalks()

	chat.Render(40)
	chat.Render(40)
	if walked := chat.RenderWalks() - warm; walked != 0 {
		t.Fatalf("warm frames walked the child list %d times, want 0", walked)
	}

	// A change must still reach the frame and take the walk.
	child.Container.Clear()
	child.Container.AddChild(NewText("world", 0, 0, nil))
	child.MarkDirty()
	if lines := chat.Render(40); !hasLine(lines, "world") {
		t.Fatalf("changed frame lines = %q", lines)
	}
	if chat.RenderWalks() == warm {
		t.Fatal("a changed frame did not walk the child list")
	}
}

// TestContainerWarmFrameKeepsTheMouseLayout guards the fast path's deferred
// mouse-layout rebuild: the layout from the walking frame must stay valid, and
// a later walk must refresh it.
func TestContainerWarmFrameKeepsTheMouseLayout(t *testing.T) {
	chat := &Container{SkipUnchangedChildren: true}
	first := &versionCountingChild{Container: &Container{}}
	first.Container.AddChild(NewText("a", 0, 0, nil))
	second := &versionCountingChild{Container: &Container{}}
	second.Container.AddChild(NewText("b", 0, 0, nil))
	second.Container.AddChild(NewText("c", 0, 0, nil))
	chat.AddChild(first)
	chat.AddChild(second)
	chat.Render(40)

	if _, layout := chat.MouseLayout(); len(layout) != 2 || layout[0].Height != 1 || layout[1].Height != 2 {
		t.Fatalf("cold layout = %+v, want heights [1 2]", layout)
	}
	chat.Render(40) // fast path, no rebuild
	if _, layout := chat.MouseLayout(); len(layout) != 2 || layout[0].Height != 1 || layout[1].Height != 2 {
		t.Fatalf("warm layout = %+v, want heights [1 2]", layout)
	}
}
