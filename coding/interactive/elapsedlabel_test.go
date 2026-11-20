package interactive

import (
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/tui"
)

// TestElapsedLabelUpdatesAcrossPaints reproduces the freeze the elapsed label
// shows while its tool call is silent.
//
// shellElapsedComponent computes the duration at render time, which is right,
// and AnimationFrame asks the loop to paint once a second, which is also right.
// But a paint only re-renders what a container believes changed, and the label
// declares no render revision. A container that caches its children's lines
// (tui/component.go's firstChangedChild) therefore keeps serving the label's
// previous line forever: the clock is never read again, and the frame the loop
// keeps painting shows a stale duration. A neighbouring animation — the spinner
// — covers its own subtree, so it keeps moving while the label sits frozen.
func TestElapsedLabelUpdatesAcrossPaints(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	InitTheme("dark", false)

	state := &shellCallState{startedAtMS: time.Now().UnixMilli()}
	elapsed := &shellElapsedComponent{state: state, theme: ActiveTheme(), partial: true}

	// The tool's shape: the label sits inside containers, as it does under the
	// tool's content box.
	contentBox := &tui.Container{}
	contentBox.AddChild(elapsed)
	painted := &tui.Container{}
	painted.AddChild(contentBox)

	first := strings.Join(painted.Render(80), "\n")
	if !strings.Contains(first, "Elapsed") {
		t.Fatalf("the label did not render at all: %q", first)
	}

	// Three seconds of a running, silent call. The loop keeps painting; nothing
	// else changes.
	state.startedAtMS -= 3000
	second := strings.Join(painted.Render(80), "\n")

	if first == second {
		t.Fatalf("the elapsed label did not update across paints:\nfirst:  %q\nsecond: %q", first, second)
	}
}

// TestRunningBashToolElapsedLabelUpdatesAcrossPaints is the tool-level
// reproduction, with the real component and the versioned parent it lives under.
//
// The loop's side is already known to work: the spinner the user sees moving
// while the label froze proves the animation tick fires and frames are written.
// So the question is whether a repaint of the tool block produces fresh text.
// The clock is advanced by moving the call's start time instead of sleeping, so
// the test is deterministic.
func TestRunningBashToolElapsedLabelUpdatesAcrossPaints(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	useBuiltinPierThemeForTest(t)
	InitTheme("dark", false)

	definition := builtinToolRenderers["bash"]
	tool := NewToolExecutionComponent("bash", "call-1",
		map[string]any{"command": "sleep 30"}, ToolExecutionOptions{}, &definition, nil, "/tmp")
	tool.MarkExecutionStarted()
	tool.SetArgsComplete()
	// A running call whose output is silent: partial, no text yet, no error.
	tool.UpdateResult(&SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: ""}}}, true)

	// The tool sits under containers, as it does in the transcript.
	painted := &tui.Container{}
	painted.AddChild(tool)

	first := strings.Join(painted.Render(80), "\n")
	if !strings.Contains(first, "Elapsed") {
		t.Fatalf("no elapsed label in the rendered tool block:\n%s", first)
	}

	// Three seconds of a silent call. Nothing else changes: no output, no
	// result, no invalidate — only the clock moved on.
	state := shellCallStateFor(&ToolRenderContext{State: tool.rendererState})
	if state == nil || state.startedAtMS == 0 {
		t.Fatal("the shell call state was not created by the first render")
	}
	state.startedAtMS -= 3000

	second := strings.Join(painted.Render(80), "\n")
	if first == second {
		t.Fatalf("REPRODUCED: the elapsed label is byte-identical across repaints, so a silent call's label stays frozen while the loop keeps painting:\n%s", second)
	}
}

// TestMcpToolTicksTheElapsedLabelAtTenths pins the MCP frame's clock to the
// shell's cadence (D238): its elapsed label prints tenths via formatDuration, so
// a one-second tick made the number jump a whole second at a time and the call
// looked frozen between them.
func TestMcpToolTicksTheElapsedLabelAtTenths(t *testing.T) {
	InitTheme("dark", false)
	mcp := mcpRenderers("fs/read_file")
	tool := NewToolExecutionComponent("mcp__fs__read_file", "call-1",
		map[string]any{"path": "/tmp/a"}, ToolExecutionOptions{}, &mcp, nil, "/tmp")
	tool.MarkExecutionStarted()
	tool.SetArgsComplete()
	tool.UpdateResult(textResult("ok"), true)
	if needs, delay := tool.AnimationFrame(time.Now()); !needs || delay != shellElapsedTick {
		t.Fatalf("MCP running tick = (%v, %s), want (true, %s)", needs, delay, shellElapsedTick)
	}

	// A running tool without the tenths label keeps the upstream one-second tick.
	read := WithBuiltInRenderers("read", nil)
	other := NewToolExecutionComponent("read", "call-2",
		map[string]any{"path": "/tmp/a"}, ToolExecutionOptions{}, read, nil, "/tmp")
	other.MarkExecutionStarted()
	other.UpdateResult(textResult("ok"), true)
	if needs, delay := other.AnimationFrame(time.Now()); !needs || delay != time.Second {
		t.Fatalf("read running tick = (%v, %s), want (true, 1s)", needs, delay)
	}
}

// TestMcpRunningCallWithoutPartialsKeepsTicking is the reported case: an MCP
// server that streams no partial results still has to show a moving clock, so
// the tool asks for animation frames from execution start to the final result,
// not only while a partial result is present.
func TestMcpRunningCallWithoutPartialsKeepsTicking(t *testing.T) {
	InitTheme("dark", false)
	renderers := mcpRenderers("fs/read_file")
	tool := NewToolExecutionComponent("mcp__fs__read_file", "call-1",
		map[string]any{"path": "/tmp/a"}, ToolExecutionOptions{}, &renderers, nil, "/tmp")
	tool.MarkExecutionStarted()
	tool.SetArgsComplete()
	if needs, delay := tool.AnimationFrame(time.Now()); !needs || delay != shellElapsedTick {
		t.Fatalf("no-result tick = (%v, %s), want (true, %s)", needs, delay, shellElapsedTick)
	}
	tool.UpdateResult(textResult("ok"), false)
	if needs, _ := tool.AnimationFrame(time.Now()); needs {
		t.Fatal("a finished MCP call still asks for animation frames")
	}
}
