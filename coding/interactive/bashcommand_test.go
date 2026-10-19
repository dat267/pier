package interactive

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// manualBashIn returns the transcript's manual `!` shell frame, if any. A `!`
// command renders through the shell tool's frame (D246).
func manualBashIn(app *App) *ToolExecutionComponent {
	for _, child := range app.chat.Children {
		if component, ok := child.(*ToolExecutionComponent); ok &&
			strings.HasPrefix(component.toolCallID, "user-bash-") {
			return component
		}
	}
	return nil
}

// bashFrameOutput is the frame's current result text.
func bashFrameOutput(component *ToolExecutionComponent) string {
	return GetTextOutput(component.result, true)
}

// pumpUntilBashOutput drains the renderer's posted callbacks — the component
// work is posted from the command's goroutine — until the output arrives.
func pumpUntilBashOutput(t *testing.T, app *App, want string) *ToolExecutionComponent {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		app.ui.RenderNow(true)
		component := manualBashIn(app)
		if component != nil && strings.Contains(bashFrameOutput(component), want) {
			return component
		}
		if time.Now().After(deadline) {
			if component == nil {
				t.Fatalf("no shell frame in the transcript")
			}
			t.Fatalf("bash output = %q, want it to contain %q", bashFrameOutput(component), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// recordedBashMessage returns the session's bash-execution message, decoded.
func recordedBashMessage(t *testing.T, app *App) (command, output string, excludeFromContext bool) {
	t.Helper()
	for _, message := range app.session.Agent.State().Messages {
		custom, ok := message.(*ai.CustomMessage)
		if !ok || custom.Role != coding.RoleBashExecution {
			continue
		}
		var fields struct {
			Command            string `json:"command"`
			Output             string `json:"output"`
			ExcludeFromContext bool   `json:"excludeFromContext"`
		}
		if err := json.Unmarshal(custom.Content, &fields); err != nil {
			t.Fatalf("decode bash message: %v", err)
		}
		return fields.Command, fields.Output, fields.ExcludeFromContext
	}
	return "", "", false
}

// pumpUntilRecorded drains the loop until the command's result has been
// recorded in the session. The output appears a callback earlier than the
// record, so waiting for output alone leaves the recording to chance.
func pumpUntilRecorded(t *testing.T, app *App, want string) (command, output string, exclude bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		app.ui.RenderNow(true)
		if command, output, exclude = recordedBashMessage(t, app); command == want {
			return command, output, exclude
		}
		if time.Now().After(deadline) {
			t.Fatalf("no recorded bash message for %q (got %q)", want, command)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// `!command` in the editor runs the shell command, streams its output into the
// transcript, and records the result in the session history.
func TestBashCommandFromTheEditor(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	run := newSubmitWiring(app).Handlers.HandleBashCommand
	if run == nil {
		t.Fatal("the submit wiring has no bash-command handler")
	}
	if err := run("echo hello-from-bash", false); err != nil {
		t.Fatal(err)
	}

	component := pumpUntilBashOutput(t, app, "hello-from-bash")
	if args, ok := component.args.(map[string]any); !ok || args["command"] != "echo hello-from-bash" {
		t.Errorf("command = %v", component.args)
	}
	// The frame is the shell tool's: the same call line and output preview a
	// model bash call renders (D246).
	rendered := coding.StripAnsi(strings.Join(component.Render(100), "\n"))
	if !strings.Contains(rendered, "$ echo hello-from-bash") || !strings.Contains(rendered, "hello-from-bash") {
		t.Fatalf("shell frame render = %q", rendered)
	}

	// The result also joins the session, which is what keeps it in the
	// transcript replay.
	command, output, exclude := pumpUntilRecorded(t, app, "echo hello-from-bash")
	if command != "echo hello-from-bash" || !strings.Contains(output, "hello-from-bash") {
		t.Errorf("recorded command = %q output = %q", command, output)
	}
	if exclude {
		t.Error("a single-! command must stay in the model's context")
	}
}

// `!!command` is the excluded form: same rendering, but the recorded result is
// kept out of the model's context.
func TestBashCommandExcludedForm(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	run := newSubmitWiring(app).Handlers.HandleBashCommand
	if err := run("echo not-for-the-model", true); err != nil {
		t.Fatal(err)
	}
	pumpUntilBashOutput(t, app, "not-for-the-model")

	_, _, exclude := pumpUntilRecorded(t, app, "echo not-for-the-model")
	if !exclude {
		t.Error("the !! form must record excludeFromContext")
	}
}

// A command that exits non-zero completes as an error result, so the shell
// frame uses the tool error background like a failed model bash call.
func TestBashCommandMarksAFailure(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	run := newSubmitWiring(app).Handlers.HandleBashCommand
	if err := run("exit 3", false); err != nil {
		t.Fatal(err)
	}

	component := waitForBashFinished(t, app)
	if component.result == nil || !component.result.IsError {
		t.Fatalf("failed command result = %+v; want IsError", component.result)
	}
}

// waitForBashFinished drains the loop until the frame's result is final. The
// recording and the final frame both happen in one posted callback.
func waitForBashFinished(t *testing.T, app *App) *ToolExecutionComponent {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		app.ui.RenderNow(true)
		if component := manualBashIn(app); component != nil && component.result != nil && !component.isPartial {
			return component
		}
		if time.Now().After(deadline) {
			t.Fatalf("the shell frame never finished; children = %d", len(app.chat.Children))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// countBashExecutionEntries counts the session's bash-execution entries: the
// transcript replay (and therefore what `pier -c` renders) is built from those.
func countBashExecutionEntries(t *testing.T, app *App) int {
	t.Helper()
	count := 0
	for _, entry := range app.session.Sessions.GetEntries() {
		if len(entry.Message) == 0 {
			continue
		}
		var message struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(entry.Message, &message); err != nil {
			t.Fatalf("decode entry message: %v", err)
		}
		if message.Role == coding.RoleBashExecution {
			count++
		}
	}
	return count
}

// TestBashCommandIsRecordedOnce pins one session entry per `!` run. The
// session's ExecuteBash records the result itself (upstream executeBash calls
// recordBashResult), so the handler must not record it again — it did, which
// wrote every run to the session twice and made a reopened session render the
// command and its output twice.
func TestBashCommandIsRecordedOnce(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	// A non-zero exit, because that is what renders the completion marker the
	// wait below keys on.
	run := newSubmitWiring(app).Handlers.HandleBashCommand
	if err := run("exit 3", false); err != nil {
		t.Fatal(err)
	}
	pumpUntilRecorded(t, app, "exit 3")
	waitForBashFinished(t, app)

	if count := countBashExecutionEntries(t, app); count != 1 {
		t.Fatalf("session holds %d bash-execution entries for one command; want 1", count)
	}
}

// The whole path, not just the handler: typing "!command" and submitting runs
// it, which is what was dead before (the seam existed, the call site existed,
// nothing was connected).
func TestBashCommandThroughSubmit(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	wiring := newSubmitWiring(app)
	wiring.HandleSubmit(context.Background(), "!echo through-submit")

	pumpUntilBashOutput(t, app, "through-submit")
	if command, _, exclude := pumpUntilRecorded(t, app, "echo through-submit"); command != "echo through-submit" || exclude {
		t.Errorf("recorded command = %q exclude = %v", command, exclude)
	}
}

// The "!!" form reaches the same handler with the exclusion flag.
func TestBashCommandExcludedThroughSubmit(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	wiring := newSubmitWiring(app)
	wiring.HandleSubmit(context.Background(), "!!echo excluded-submit")

	pumpUntilBashOutput(t, app, "excluded-submit")
	if _, _, exclude := pumpUntilRecorded(t, app, "echo excluded-submit"); !exclude {
		t.Error("the !! form did not reach the handler as excluded")
	}
}

// `/new` starts a fresh session and says so. The command's NewSession seam was
// never assigned, so it cleared the editor and returned silently.
func TestNewCommandStartsASession(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	before := app.sessionMgr.GetSessionID()
	app.commands.HandleClearCommand(context.Background())

	if after := app.sessionMgr.GetSessionID(); after == before {
		t.Errorf("session id unchanged (%q)", after)
	}
	found := false
	for _, text := range transcriptTexts(app) {
		if strings.Contains(text, "New session started") {
			found = true
		}
	}
	if !found {
		t.Errorf("no confirmation in the transcript: %q", transcriptTexts(app))
	}
}

// Upstream trims the bash command with String.trim, so a leading U+FEFF is
// dropped before the command runs.
func TestBashCommandTrimsJSUrFEFF(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	wiring := newSubmitWiring(app)
	wiring.HandleSubmit(context.Background(), "!\uFEFFecho js-ws")

	pumpUntilBashOutput(t, app, "js-ws")
	if command, _, _ := pumpUntilRecorded(t, app, "echo js-ws"); command != "echo js-ws" {
		t.Errorf("recorded command = %q", command)
	}
}

// A `!` command joins the transcript as soon as it is triggered, even while
// the model streams: the frame shows its loader and streaming output in the
// main window instead of waiting in the footer dock (D246). The dock does not
// grow, so the transcript keeps its height for the whole run.
func TestBashFrameJoinsTheTranscriptWhileTheModelStreams(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()

	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseTurn := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseTurn()

	app.session.Agent.StreamFunction = func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			<-release
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: model.Provider, Model: model.ID,
				Content: ai.ContentList{ai.TextContent{Text: "still working"}}, StopReason: ai.StopStop,
			}})
		}()
		return stream
	}
	go func() { _ = app.session.Prompt(context.Background(), "keep working", nil) }()

	waitUntil := func(timeout time.Duration, what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			app.ui.RenderNow(true)
			time.Sleep(2 * time.Millisecond)
		}
	}
	waitUntil(5*time.Second, "the session to start streaming", func() bool { return app.session.IsStreaming() })

	var output strings.Builder
	for i := 0; i < 30; i++ {
		output.WriteString("dua output line\n")
	}
	command := "sleep 0.3\ncat <<'EOF'\n" + output.String() + "EOF"
	if err := newSubmitWiring(app).Handlers.HandleBashCommand(command, false); err != nil {
		t.Fatal(err)
	}

	waitUntil(5*time.Second, "the running frame to join the transcript", func() bool {
		return manualBashIn(app) != nil
	})
	if !app.session.IsBashRunning() {
		t.Fatal("the command finished before the running frame could be checked")
	}

	waitUntil(15*time.Second, "the bash command to finish", func() bool { return !app.session.IsBashRunning() })
	// Drain the posted completion callback.
	for end := time.Now().Add(200 * time.Millisecond); time.Now().Before(end); {
		app.ui.RenderNow(true)
		time.Sleep(2 * time.Millisecond)
	}

	if manualBashIn(app) == nil {
		t.Fatal("the completed shell frame is not in the transcript")
	}

	screen, ok := app.lifecycle.CurrentUI().(*tui.AltScreen)
	if !ok {
		t.Fatalf("UI = %T", app.lifecycle.CurrentUI())
	}
	frame := tui.RenderLayoutFrame(screen.LayoutRoot(), 100, 30, func() {})
	box, ok := tui.GetScrollViewBox(frame, app.transcriptScrollView)
	if !ok {
		t.Fatal("no transcript scroll box")
	}
	if box.Rect.Height < 15 {
		t.Fatalf("transcript height = %d of 30 rows; the dock swallowed the screen", box.Rect.Height)
	}
}

// A running `!` frame keeps ticking while the agent is idle: the shell tool's
// elapsed label must advance even though nothing else asks for a repaint (the
// frame is a direct transcript child, so its animation tick reaches the chat's
// revision cache, D229).
func TestManualBashFrameTicksWhileTheAgentIsIdle(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	stop := runTestAppLoop(t, app)
	defer stop()

	if err := newSubmitWiring(app).Handlers.HandleBashCommand("sleep 2", false); err != nil {
		t.Fatal(err)
	}
	waitForConditionWithin(t, func() bool { return manualBashIn(app) != nil }, 5*time.Second)

	screen := app.lifecycle.CurrentUI().(*tui.AltScreen)
	elapsedLine := func() string {
		frame := tui.RenderLayoutFrame(screen.LayoutRoot(), 100, 30, func() {})
		for _, line := range frame.Lines {
			if plain := coding.StripAnsi(line); strings.Contains(plain, "Elapsed") {
				return plain
			}
		}
		return ""
	}
	var first string
	waitForLabel := time.Now().Add(2 * time.Second)
	for first = postValue(app, elapsedLine); first == ""; first = postValue(app, elapsedLine) {
		if time.Now().After(waitForLabel) {
			t.Fatal("the running frame has no elapsed label")
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline := time.Now().Add(1500 * time.Millisecond)
	for {
		line := postValue(app, elapsedLine)
		if line != first {
			if !strings.Contains(line, "Elapsed") {
				t.Fatalf("the run finished before the label advanced: %q -> %q", first, line)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the elapsed label did not advance while the agent was idle: %q", first)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Escape cancels a manual command together with the turn it runs alongside:
// the `!` frame is part of the active turn's lifecycle, not an independent job
// (HandleEscape's streaming branch aborts the run, which aborts the bash runs).
func TestEscapeCancelsTheManualCommandWithTheTurn(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	stop := runTestAppLoop(t, app)
	defer stop()

	app.session.Agent.StreamFunction = func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			select {
			case <-options.Ctx.Done():
			case <-time.After(5 * time.Second):
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopAborted, Message: &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: model.Provider, Model: model.ID,
				Content: ai.ContentList{ai.TextContent{Text: "stopped"}}, StopReason: ai.StopAborted,
			}})
		}()
		return stream
	}
	go func() { _ = app.session.Prompt(context.Background(), "keep working", nil) }()
	waitForConditionWithin(t, func() bool { return app.session.IsStreaming() }, 10*time.Second)

	if err := newSubmitWiring(app).Handlers.HandleBashCommand("sleep 10", false); err != nil {
		t.Fatal(err)
	}
	waitForConditionWithin(t, func() bool { return app.session.IsBashRunning() }, 10*time.Second)

	postValue(app, func() int {
		newKeyWiring(app).HandleEscape(func() int64 { return time.Now().UnixMilli() })
		return 0
	})
	waitForConditionWithin(t, func() bool {
		return !app.session.IsBashRunning() && !app.session.IsStreaming()
	}, 10*time.Second)
}
