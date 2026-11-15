package tui

import "testing"

// Reference: pi v1.5.2 (b4afdddf) tui/src/tui.ts:1033-1039: requestRender(force)
// resets the screen's differential render state before scheduling the paint, so
// a caller that returns to the terminal (external editor, SIGCONT resume, a
// reload overlay) repaints the whole screen instead of diffing against the
// frame from before the stop. Pier dropped the argument and only signalled a
// tick, so those three call sites kept the stale baseline.
func TestRequestRenderWithForceResetsRenderState(t *testing.T) {
	renderer := NewRenderer(&fakeTerminal{width: 40, height: 10})
	resets := 0
	renderer.OnResetRenderState = func() { resets++ }

	renderer.RequestRender(false)
	if resets != 0 {
		t.Fatalf("RequestRender(false) reset the render state %d times, want 0", resets)
	}
	renderer.RequestRender(true)
	if resets != 1 {
		t.Fatalf("RequestRender(true) reset the render state %d times, want 1", resets)
	}

	select {
	case <-renderer.RenderTicks():
	default:
		t.Fatal("RequestRender(true) did not request a paint")
	}
}
