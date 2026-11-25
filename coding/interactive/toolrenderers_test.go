package interactive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

func homeDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func newRendererTestTheme(t *testing.T) *Theme {
	t.Helper()
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)
	return ActiveTheme()
}

func renderCall(name string, args any, theme *Theme) string {
	renderers := WithBuiltInRenderers(name, nil)
	if renderers == nil {
		return ""
	}
	component := renderers.RenderCall(args, theme, &ToolRenderContext{Cwd: "/tmp/proj"})
	return coding.StripAnsi(strings.Join(component.Render(120), "\n"))
}

func renderResult(name string, result *SortToolResultContent, expanded bool, theme *Theme) string {
	renderers := WithBuiltInRenderers(name, nil)
	if renderers == nil {
		return ""
	}
	component := renderers.RenderResult(result, ToolRenderResultOptions{Expanded: expanded}, theme,
		&ToolRenderContext{Cwd: "/tmp/proj", Args: nil, ShowImages: false})
	return coding.StripAnsi(strings.Join(component.Render(120), "\n"))
}

func textResult(text string) *SortToolResultContent {
	return &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: text}}}
}

// TestToolRenderersCallHeaders covers the upstream call headers (the ones that
// used to render as raw JSON).
func TestToolRenderersCallHeaders(t *testing.T) {
	theme := newRendererTestTheme(t)

	args := func(raw string) any { return json.RawMessage(raw) }

	if got := renderCall("read", args(`{"file_path":"/tmp/proj/main.go"}`), theme); !strings.Contains(got, "read /tmp/proj/main.go") {
		t.Fatalf("read call = %q", got)
	}
	// Build the home case with json.Marshal: concatenating a Windows path into
	// JSON leaves its backslashes unescaped (\U, \n), which is invalid JSON and
	// renders as the empty-path fallback. upstream shortenPath swaps the home
	// prefix for "~" and keeps the remainder verbatim, so the separator here is
	// the platform's.
	homePath := filepath.Join(homeDir(t), "notes.md")
	homeArgs, err := json.Marshal(map[string]string{"file_path": homePath})
	if err != nil {
		t.Fatal(err)
	}
	if got := renderCall("read", args(string(homeArgs)), theme); !strings.Contains(got, "read ~"+strings.TrimPrefix(homePath, homeDir(t))) {
		t.Fatalf("read home call = %q", got)
	}
	if got := renderCall("read", args(`{"file_path":"/tmp/proj/main.go","offset":10,"limit":5}`), theme); !strings.Contains(got, ":10-14") {
		t.Fatalf("read call range = %q", got)
	}
	if got := renderCall("read", args(`{"file_path":"/tmp/proj/main.go","offset":1.5,"limit":1.5}`), theme); !strings.Contains(got, ":1.5-2") {
		t.Fatalf("fractional read call range = %q", got)
	}
	if got := renderCall("read", args(`{"file_path":"/tmp/proj/main.go","limit":0}`), theme); !strings.Contains(got, ":1 ") || strings.Contains(got, ":1-0") {
		t.Fatalf("zero read limit range = %q", got)
	}
	if got := renderCall("bash", args(`{"command":"ls -la"}`), theme); !strings.Contains(got, "$ ls -la") {
		t.Fatalf("bash call = %q", got)
	}
	if got := renderCall("grep", args(`{"pattern":"TODO","path":"/tmp/proj/src"}`), theme); !strings.Contains(got, "grep /TODO/ in /tmp/proj/src") {
		t.Fatalf("grep call = %q", got)
	}
	if got := renderCall("grep", args(`{"pattern":"TODO","limit":1.5}`), theme); !strings.Contains(got, "limit 1.5") {
		t.Fatalf("fractional grep call = %q", got)
	}
	if got := renderCall("find", args(`{"pattern":"*.go"}`), theme); !strings.Contains(got, "find *.go in .") {
		t.Fatalf("find call = %q", got)
	}
	if got := renderCall("find", args(`{"pattern":"*.go","limit":1.5}`), theme); !strings.Contains(got, "limit 1.5") {
		t.Fatalf("fractional find call = %q", got)
	}
	if got := renderCall("ls", args(`{}`), theme); !strings.Contains(got, "ls .") {
		t.Fatalf("ls call = %q", got)
	}
	if got := renderCall("ls", args(`{"limit":1.5}`), theme); !strings.Contains(got, "limit 1.5") {
		t.Fatalf("fractional ls call = %q", got)
	}
	if got := renderCall("write", args(`{"file_path":"/tmp/proj/a.go","content":"package main"}`), theme); !strings.Contains(got, "write /tmp/proj/a.go") {
		t.Fatalf("write call = %q", got)
	}
	if got := renderCall("edit", args(`{"file_path":"/tmp/proj/a.go","oldText":"a","newText":"b"}`), theme); !strings.Contains(got, "edit /tmp/proj/a.go") {
		t.Fatalf("edit call = %q", got)
	}
	// No raw JSON in any header.
	for _, name := range BuiltinToolRendererNames() {
		got := renderCall(name, args(`{"file_path":"/tmp/proj/main.go"}`), theme)
		if strings.Contains(got, "{") && strings.Contains(got, "\"file_path\"") {
			t.Errorf("%s call renders raw JSON: %q", name, got)
		}
	}
}

// TestToolRenderersResults covers the collapsed/expanded result bodies.
func TestToolRenderersResults(t *testing.T) {
	theme := newRendererTestTheme(t)

	// Collapsed (not expanded) results are empty for read.
	if got := renderResult("read", textResult("hello"), false, theme); strings.Contains(got, "hello") {
		t.Fatalf("collapsed read result should be empty: %q", got)
	}
	expanded := renderResult("read", textResult("hello"), true, theme)
	if !strings.Contains(expanded, "hello") {
		t.Fatalf("expanded read result = %q", expanded)
	}

	// ls collapses to 20 lines with a truncation hint.
	many := make([]string, 30)
	for index := range many {
		many[index] = "entry"
	}
	lsResult := renderResult("ls", textResult(strings.Join(many, "\n")), false, theme)
	if !strings.Contains(lsResult, "... (10 more lines,") {
		t.Fatalf("ls truncation hint missing: %q", lsResult)
	}

	// Truncation details surface a warning.
	details := &SortToolResultContent{
		Content: []ToolResultContent{{Type: "text", Text: "out"}},
		Details: &coding.LsToolDetails{Truncation: &coding.TruncationResult{
			Truncated: true, TruncatedBy: "bytes", OutputLines: 20, MaxBytes: 50 * 1024,
		}},
	}
	if got := renderResult("ls", details, false, theme); !strings.Contains(got, "[Truncated:") {
		t.Fatalf("ls truncation warning missing: %q", got)
	}
}

// TestEditRendererPreview covers the edit preview diff.
func TestEditRendererPreviewReplacesMalformedUTF8(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "invalid.txt")
	if err := os.WriteFile(file, []byte{0xff, '\n', 't', 'a', 'r', 'g', 'e', 't', '\n'}, 0o644); err != nil {
		t.Fatal(err)
	}
	preview := computeEditsPreview(file, []coding.Edit{{OldText: "target", NewText: "changed"}}, dir)
	if preview == nil || preview.Error != "" || !strings.Contains(preview.Diff, "\uFFFD") {
		t.Fatalf("edit preview = %+v; want replacement-decoded context", preview)
	}
}

func TestEditRendererPreview(t *testing.T) {
	newRendererTestTheme(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	preview := computeEditsPreview(file, []coding.Edit{{OldText: "two", NewText: "TWO"}}, dir)
	if preview == nil || preview.Error != "" {
		t.Fatalf("preview = %+v", preview)
	}
	if !strings.Contains(preview.Diff, "TWO") || !strings.Contains(preview.Diff, "one") {
		t.Fatalf("preview diff = %q", preview.Diff)
	}
	if preview.FirstChangedLine == nil || *preview.FirstChangedLine != 2 {
		t.Fatalf("firstChangedLine = %+v", preview.FirstChangedLine)
	}

	// A missing file reports the upstream error shape.
	missing := computeEditsPreview(filepath.Join(dir, "gone.txt"), []coding.Edit{{OldText: "a", NewText: "b"}}, dir)
	if missing == nil || !strings.Contains(missing.Error, "Could not edit file") {
		t.Fatalf("missing preview = %+v", missing)
	}
}

// TestWithBuiltInRenderers covers the merge semantics.
func TestWithBuiltInRenderers(t *testing.T) {
	// Unknown tools pass the definition through (nil stays nil).
	if got := WithBuiltInRenderers("unknown-tool", nil); got != nil {
		t.Fatalf("unknown tool = %+v", got)
	}
	custom := &ToolRenderers{RenderShell: "self"}
	if got := WithBuiltInRenderers("unknown-tool", custom); got != custom {
		t.Fatalf("unknown tool with definition = %+v", got)
	}
	// Built-in merge keeps the custom call renderer and fills the result.
	merged := WithBuiltInRenderers("read", &ToolRenderers{RenderShell: "self"})
	if merged == nil || merged.RenderShell != "self" || merged.RenderCall == nil || merged.RenderResult == nil {
		t.Fatalf("merged = %+v", merged)
	}
}

// TestToolRendererShellComponent verifies the collapsed bash preview component
// renders the tail of the output.
func TestToolRendererShellComponent(t *testing.T) {
	theme := newRendererTestTheme(t)
	lines := make([]string, 40)
	for index := range lines {
		lines[index] = "line"
	}
	result := textResult(strings.Join(lines, "\n"))
	component := bashRenderers.RenderResult(result, ToolRenderResultOptions{Expanded: false}, theme,
		&ToolRenderContext{Cwd: "/tmp/proj"})
	rendered := coding.StripAnsi(strings.Join(component.Render(120), "\n"))
	if !strings.Contains(rendered, "earlier lines") {
		t.Fatalf("bash preview = %q", rendered)
	}
	if !strings.Contains(rendered, "line") {
		t.Fatalf("bash preview missing output: %q", rendered)
	}
	_ = tui.NewSpacer(1)
}

// TestShellElapsedDuration covers the running-time display for shell tools: a
// partial result keeps the timer running and the label keeps ticking (upstream
// arms a 1s invalidate), and the final result freezes it as "Took". The port
// set endedAtMS on the *partial* update (inverting upstream's
// `!isPartial || isError`), so the timer froze at the first update, and it
// never armed a redraw, so the elapsed value never advanced.
func TestShellElapsedDuration(t *testing.T) {
	theme := newRendererTestTheme(t)
	ctx := &ToolRenderContext{Cwd: "/tmp/proj", Args: map[string]any{"command": "sleep 5"}, ExecutionStarted: true}
	bashRenderers.RenderCall(ctx.Args, theme, ctx)
	state, ok := ctx.State.(*shellCallState)
	if !ok || state.startedAtMS == 0 {
		t.Fatalf("execution start did not start the timer: %+v", ctx.State)
	}

	result := textResult("partial output")
	component := bashRenderers.RenderResult(result, ToolRenderResultOptions{IsPartial: true}, theme, ctx)
	if state.endedAtMS != 0 {
		t.Fatal("a partial result must not stop the timer")
	}

	// The running label recomputes from the clock on every render.
	state.startedAtMS = time.Now().Add(-5 * time.Second).UnixMilli()
	rendered := coding.StripAnsi(strings.Join(component.Render(120), "\n"))
	if !strings.Contains(rendered, "Elapsed 5.") {
		t.Fatalf("running duration = %q, want an Elapsed 5.x", rendered)
	}

	// The component animates while the call runs so the label keeps ticking.
	container, ok := component.(*tui.Container)
	if !ok {
		t.Fatalf("result component = %T", component)
	}
	animated := false
	for _, child := range container.Children {
		if animator, ok := child.(tui.Animator); ok {
			want, delay := animator.AnimationFrame(time.Now())
			// D238: the port ticks the elapsed label at 100ms so its tenths
			// display resolution moves by .1s (upstream redraws at 1000ms).
			if !want || delay != shellElapsedTick {
				t.Fatalf("running animator = want %v delay %v, want a %v tick", want, delay, shellElapsedTick)
			}
			animated = true
		}
	}
	if !animated {
		t.Fatal("running tool has no animator driving the elapsed label")
	}

	// The final result freezes the label as "Took".
	component = bashRenderers.RenderResult(result, ToolRenderResultOptions{}, theme, ctx)
	if state.endedAtMS == 0 {
		t.Fatal("the final result must stop the timer")
	}
	rendered = coding.StripAnsi(strings.Join(component.Render(120), "\n"))
	if !strings.Contains(rendered, "Took ") {
		t.Fatalf("final duration = %q, want a Took line", rendered)
	}
	for _, child := range component.(*tui.Container).Children {
		if animator, ok := child.(tui.Animator); ok {
			if want, _ := animator.AnimationFrame(time.Now()); want {
				t.Fatal("a finished tool must not keep animating")
			}
		}
	}
}

// #10549: the label shows the tool's recorded execution time, so a replayed result (which has
// no start or end event and therefore no timestamps) still reports Took, and a live call does
// not count the wall-clock steps between the events.
func TestShellElapsedUsesTheRecordedDuration(t *testing.T) {
	theme := newRendererTestTheme(t)
	recorded := int64(1500)

	// Replay: the session re-renders the tool result with its recorded duration and nothing else.
	ctx := &ToolRenderContext{Cwd: "/tmp/proj", Args: map[string]any{"command": "sleep 1.5"}, DurationMs: &recorded}
	component := bashRenderers.RenderResult(textResult("done"), ToolRenderResultOptions{}, theme, ctx)
	rendered := coding.StripAnsi(strings.Join(component.Render(120), "\n"))
	if !strings.Contains(rendered, "Took 1.5s") {
		t.Fatalf("replayed label = %q, want Took 1.5s", rendered)
	}
	if strings.Contains(rendered, "Elapsed") {
		t.Fatalf("a replayed result must not show a running label: %q", rendered)
	}

	// Live: the wall clock says five seconds, the tool reported 1.5.
	ctx = &ToolRenderContext{Cwd: "/tmp/proj", Args: map[string]any{"command": "sleep 1.5"}, ExecutionStarted: true}
	bashRenderers.RenderCall(ctx.Args, theme, ctx)
	state, ok := ctx.State.(*shellCallState)
	if !ok {
		t.Fatalf("state = %T", ctx.State)
	}
	state.startedAtMS = time.Now().Add(-5 * time.Second).UnixMilli()
	ctx.DurationMs = &recorded
	component = bashRenderers.RenderResult(textResult("done"), ToolRenderResultOptions{}, theme, ctx)
	rendered = coding.StripAnsi(strings.Join(component.Render(120), "\n"))
	if !strings.Contains(rendered, "Took 1.5s") || strings.Contains(rendered, "Took 5.") {
		t.Fatalf("live label = %q, want the recorded 1.5s rather than five seconds of wall clock", rendered)
	}
}

// Upstream's recorded-duration branch requires !isPartial, so a partial result
// that carries a duration still shows "Elapsed".
func TestShellElapsedPartialWithDurationStaysElapsed(t *testing.T) {
	theme := newRendererTestTheme(t)
	ctx := &ToolRenderContext{Cwd: "/tmp/proj", Args: map[string]any{"command": "sleep 5"}, ExecutionStarted: true}
	bashRenderers.RenderCall(ctx.Args, theme, ctx)
	duration := int64(5000)
	ctx.DurationMs = &duration
	component := bashRenderers.RenderResult(textResult("partial output"), ToolRenderResultOptions{IsPartial: true}, theme, ctx)
	rendered := coding.StripAnsi(strings.Join(component.Render(120), "\n"))
	if !strings.Contains(rendered, "Elapsed ") || strings.Contains(rendered, "Took ") {
		t.Fatalf("partial with duration = %q; want Elapsed", rendered)
	}
}

// pi v1.5.0 tool-execution.ts #9265: argument deltas mark the card pending and
// the next render applies them, so a call streaming many deltas between frames
// runs its renderer once per frame instead of once per delta.
func TestToolCardAppliesArgumentDeltasOncePerRender(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	renders := 0
	definition := &ToolRenderers{RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
		renders++
		return tui.NewText("call", 0, 0, nil)
	}}
	component := NewToolExecutionComponent("bash", "call-1", map[string]any{"command": "ls"},
		ToolExecutionOptions{}, definition, nil, "/tmp")
	renders = 0
	for i := range 20 {
		component.UpdateArgs(map[string]any{"command": "ls", "extra": i})
	}
	if renders != 0 {
		t.Fatalf("argument deltas rebuilt the card %d time(s) before a render", renders)
	}
	component.Render(80)
	if renders != 1 {
		t.Fatalf("render ran the card renderer %d time(s), want 1", renders)
	}
}

// TestStreamingBashCommandTabRendersEarly covers the reported symptom: a tab in
// a streamed command only rendered once the call finished. The decode side is
// pinned in ai.TestParseStreamingEscapes; here the arguments arrive as the ai
// layer delivers them after that decode (a repaired document whose command
// holds a real tab), and the header must render it expanded and stay stable.
func TestStreamingBashCommandTabRendersEarly(t *testing.T) {
	dispatcher, _, transcript, _ := newEventTestDispatcher(t)
	transcript.Session = builtinRendererSession{}

	bashCall := func(arguments string) *ai.AssistantMessage {
		return &ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopToolUse,
			Content: ai.ContentList{ai.ToolCall{ID: "t1", Name: "bash", Arguments: json.RawMessage(arguments)}},
		}
	}
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionMessageStart, Agent: agentEvent("message_start", &ai.AssistantMessage{StopReason: ai.StopToolUse}),
	})

	// Mid-stream: the ai layer has decoded the partial JSON (the command holds
	// a real tab) and re-encoded it.
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionMessageUpdate, Agent: agentEvent("message_update", bashCall(`{"command":"echo\tfoo"}`)),
	})
	streaming := coding.StripAnsi(strings.Join(renderChat(t, transcript.Chat), "\n"))
	if !strings.Contains(streaming, "echo   foo") {
		t.Fatalf("streaming header = %q, want the tab expanded to three spaces", streaming)
	}
	if strings.Contains(streaming, `echo\tfoo`) {
		t.Fatalf("streaming header kept the escape verbatim: %q", streaming)
	}

	// Completed: the header must not change.
	dispatcher.HandleEvent(&coding.SessionEvent{
		Type: coding.SessionMessageUpdate, Agent: agentEvent("message_update", bashCall(`{"command":"echo\tfoo"}`)),
	})
	completed := coding.StripAnsi(strings.Join(renderChat(t, transcript.Chat), "\n"))
	if !strings.Contains(completed, "echo   foo") {
		t.Fatalf("completed header = %q", completed)
	}
	if streaming != completed {
		t.Fatalf("header changed on completion:\n streaming %q\n completed %q", streaming, completed)
	}
}

// allocatedBytesPerRunI is the coding package's allocatedBytesPerRun for this
// package: the delta path must not scale with transcript size.
func allocatedBytesPerRunI(t *testing.T, runs int, fn func()) int64 {
	t.Helper()
	fn()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		fn()
	}
	runtime.ReadMemStats(&after)
	return int64(after.TotalAlloc-before.TotalAlloc) / int64(runs)
}

// A streaming delta's on-loop work must not depend on how long the transcript
// is. The stalls found in the wild were exactly this class of bug (GetSessionName
// copied every entry per paint; GetContextUsage re-walked the branch per paint),
// and this pins the delta path against it: 20 deltas at a small transcript and
// at a large one must allocate within a bounded ratio.
func TestStreamingDeltaCostIndependentOfTranscriptSize(t *testing.T) {
	run := func(warm int) int64 {
		app, cleanup := newTestAppB(t)
		defer cleanup()
		screen, _ := app.initialUI.(*tui.AltScreen)
		screen.Start()
		for i := range warm {
			a := &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopStop,
				Content: ai.ContentList{ai.TextContent{Text: fmt.Sprintf("reply %d with some words that wrap across the viewport\n", i)}}}
			app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", a)})
			app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageEnd, Agent: agentEvent("message_end", a)})
		}
		streaming := &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopPending}
		app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", streaming)})
		body := ""
		return allocatedBytesPerRunI(t, 3, func() {
			for range 20 {
				body += "appended sentence with **bold** and `code`.\n\n"
				app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageUpdate, Agent: agentEvent("message_update", &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopPending, Content: ai.ContentList{ai.TextContent{Text: body}}})})
				app.ui.RenderNow(true)
			}
		})
	}
	small := run(100)
	large := run(3000)
	t.Logf("delta allocations: %d bytes at 100 messages, %d at 3000", small, large)
	if large > small*4+256*1024 {
		t.Fatalf("streaming delta allocates %d bytes at 3000 messages vs %d at 100: the cost scales with transcript size", large, small)
	}
}

// sanitizeArgValue must keep every argument, so a key that carries a terminal
// control cannot collide with its own stripped form. Two distinct keys used to
// collapse into one entry, and the renderer then read the wrong argument: the
// lookup below found 2 where the model sent 1.
func TestSanitizeArgValueKeepsDistinctKeys(t *testing.T) {
	raw := []byte(`{"a\u001b[31mb":1,"ab":2}`)
	args := toolArgs(raw)
	if len(args) != 2 {
		t.Fatalf("toolArgs = %#v; want both keys", args)
	}
	if _, ok := args["a\x1b[31mb"]; !ok {
		t.Errorf("the control key is gone: %#v", args)
	}
	if value := args["ab"]; value != float64(2) {
		t.Errorf(`args["ab"] = %#v; want 2`, value)
	}
	// A value is still sanitized for display.
	sanitized := toolArgs([]byte(`{"command":"a\u001b[31mb"}`))
	if got := sanitized["command"]; got != "ab" {
		t.Errorf("command = %#v; want the stripped value", got)
	}
}
