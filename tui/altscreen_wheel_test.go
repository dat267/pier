package tui

import "testing"

// wheelRecorder records the wheel deltas a component receives.
type wheelRecorder struct {
	deltas []int
}

func (c *wheelRecorder) Render(width int) []string { return []string{"wheel target"} }
func (c *wheelRecorder) Invalidate()               {}

func (c *wheelRecorder) HandleMouse(event TuiMouseEvent) *TuiMouseDispatchResult {
	if event.Type != MouseWheel {
		return nil
	}
	c.deltas = append(c.deltas, event.WheelDelta)
	return &TuiMouseDispatchResult{TuiMouseEventResult: TuiMouseEventResult{Handled: true}}
}

type focusedWheelOverlay struct {
	focused bool
	inputs  []string
}

func (o *focusedWheelOverlay) Render(int) []string     { return []string{"overlay"} }
func (o *focusedWheelOverlay) Invalidate()             {}
func (o *focusedWheelOverlay) SetFocused(focused bool) { o.focused = focused }
func (o *focusedWheelOverlay) IsFocused() bool         { return o.focused }
func (o *focusedWheelOverlay) HandleInput(input string) {
	o.inputs = append(o.inputs, input)
}

func TestAltScreenUnhandledWheelScrollsBehindFocusedOverlay(t *testing.T) {
	terminal := &recordingTerminal{width: 20, height: 6}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	screen.AddChild(NewText("line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10\nline 11\nline 12", 0, 0, nil))
	screen.Start()
	screen.RenderNow(false)
	before := screen.ViewportTop()
	if before <= 0 {
		t.Fatalf("initial viewport top = %d, want a scrolled transcript", before)
	}

	overlay := &focusedWheelOverlay{}
	screen.ShowOverlay(overlay, &OverlayOptions{Anchor: OverlayAnchorTopLeft, Width: intPtr(10)})
	screen.HandleTerminalInput("\x1b[<64;3;1M")

	if got := screen.ViewportTop(); got >= before {
		t.Fatalf("viewport top = %d after wheel, want less than %d", got, before)
	}
	if len(overlay.inputs) != 0 {
		t.Fatalf("focused overlay received raw wheel input: %q", overlay.inputs)
	}
}

// Port of the "applies runtime wheel line count updates" case in upstream
// tui-alt-screen.test.ts (f1927c2d5): the line count can change at runtime, and
// Alt keeps its five-times multiplier.
func TestAltScreenRuntimeWheelScrollLines(t *testing.T) {
	terminal := &recordingTerminal{width: 20, height: 4}
	initial := FixedWheelScrollLines(3)
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{WheelScrollLines: &initial})
	screen.DisableAutoRender()
	recorder := &wheelRecorder{}
	screen.AddChild(recorder)
	screen.Start()
	screen.RenderNow(false)

	screen.HandleTerminalInput("\x1b[<64;1;1M") // wheel up: -3
	screen.SetWheelScrollLines(FixedWheelScrollLines(2))
	screen.HandleTerminalInput("\x1b[<65;1;1M") // wheel down: +2
	screen.HandleTerminalInput("\x1b[<72;1;1M") // alt+wheel up: -10
	want := []int{-3, 2, -10}
	if len(recorder.deltas) != len(want) {
		t.Fatalf("deltas = %v (want %v)", recorder.deltas, want)
	}
	for i := range want {
		if recorder.deltas[i] != want[i] {
			t.Fatalf("deltas = %v (want %v)", recorder.deltas, want)
		}
	}
}
