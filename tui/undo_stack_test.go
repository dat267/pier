package tui

import "testing"

// Upstream's UndoStack.pop() in packages/tui/src/undo-stack.ts removes the
// popped element from the array. The Go stack must likewise release references
// after undo, so large snapshots do not stay live in unused slice capacity.
func TestUndoStackPopReleasesPoppedSnapshot(t *testing.T) {
	value := "large editor snapshot"
	var stack UndoStack[*string]
	stack.Push(&value)

	popped, ok := stack.Pop()
	if !ok || popped != &value {
		t.Fatalf("Pop() = (%p, %v), want (%p, true)", popped, ok, &value)
	}
	if retained := stack.stack[:cap(stack.stack)]; retained[0] != nil {
		t.Fatalf("popped snapshot remains retained in stack capacity: %p", retained[0])
	}
}
