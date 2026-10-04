package interactive

import (
	"testing"
	"time"
)

// TestToolExecutionComponentAnimatesWhileRunning pins the tool-level animation:
// a running shell tool reports a shellElapsedTick frame so the elapsed label
// repaints, every other running tool keeps the 1s frame, and a finished or
// not-yet-started tool does not.
func TestToolExecutionComponentAnimatesWhileRunning(t *testing.T) {
	component := &ToolExecutionComponent{}
	if needs, _ := component.AnimationFrame(time.Now()); needs {
		t.Fatal("a tool that has not started animates")
	}
	component.executionStarted = true
	component.isPartial = true
	needs, delay := component.AnimationFrame(time.Now())
	if !needs || delay != time.Second {
		t.Fatalf("running tool: needs=%v delay=%v", needs, delay)
	}

	shell := &ToolExecutionComponent{toolName: "bash", executionStarted: true, isPartial: true}
	if needs, delay := shell.AnimationFrame(time.Now()); !needs || delay != shellElapsedTick {
		t.Fatalf("running shell tool: needs=%v delay=%v, want %v", needs, delay, shellElapsedTick)
	}
	component.isPartial = false
	if needs, _ := component.AnimationFrame(time.Now()); needs {
		t.Fatal("a finished tool animates")
	}
}
