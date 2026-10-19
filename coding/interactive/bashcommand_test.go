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

// bashComponentIn returns the transcript's bash component, if any.
func bashComponentIn(app *App) *BashExecutionComponent {
	for _, child := range app.chat.Children {
		if component, ok := child.(*BashExecutionComponent); ok {
			return component
		}
	}
	return nil
}

// pumpUntilBashOutput drains the renderer's posted callbacks — the component
// work is posted from the command's goroutine — until the output arrives.
func pumpUntilBashOutput(t *testing.T, app *App, want string) *BashExecutionComponent {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		app.ui.RenderNow(true)
		component := bashComponentIn(app)
		if component != nil && strings.Contains(component.GetOutput(), want) {
			return component
		}
		if time.Now().After(deadline) {
			if component == nil {
				t.Fatalf("no bash component in the transcript")
			}
			t.Fatalf("bash output = %q, want it to contain %q", component.GetOutput(), want)
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
	if component.GetCommand() != "echo hello-from-bash" {
		t.Errorf("command = %q", component.GetCommand())
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

// A command that exits non-zero still completes its component, showing the exit
// code rather than looking like it succeeded.
func TestBashCommandShowsTheExitCode(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	run := newSubmitWiring(app).Handlers.HandleBashCommand
	if err := run("exit 3", false); err != nil {
		t.Fatal(err)
	}

	waitForBashExitCode(t, app, "(exit 3)")
}

// waitForBashExitCode drains the loop until the component renders its exit
// code. SetComplete and the recording both happen in one posted callback, so
// the rendered exit code means every record that is going to happen has
// happened.
func waitForBashExitCode(t *testing.T, app *App, marker string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		app.ui.RenderNow(true)
		if component := bashComponentIn(app); component != nil {
			if rendered := strings.Join(component.Render(80), "\n"); strings.Contains(rendered, marker) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the component never showed %q; children = %d", marker, len(app.chat.Children))
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
	waitForBashExitCode(t, app, "(exit 3)")

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

// pendingBashIn returns the bash frame currently attached to the dock.
func pendingBashIn(app *App) *BashExecutionComponent {
	for _, child := range app.pendingMessages.Children {
		if component, ok := child.(*BashExecutionComponent); ok {
			return component
		}
	}
	return nil
}

// A `!` command started while the model streams is shown in the pending dock.
// Once it completes the frame has to move into the transcript: the dock keeps
// the height its children ask for, so a completed 20-line preview left there
// squeezes the transcript to a single row, covering the screen and gluing the
// frame above the footer until the next normal submission.
func TestCompletedPendingBashFrameMovesToTheTranscript(t *testing.T) {
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
	command := "cat <<'EOF'\n" + output.String() + "EOF"
	if err := newSubmitWiring(app).Handlers.HandleBashCommand(command, false); err != nil {
		t.Fatal(err)
	}

	waitUntil(15*time.Second, "the bash command to finish", func() bool {
		return !app.session.IsBashRunning() && (bashComponentIn(app) != nil || pendingBashIn(app) != nil)
	})
	// Drain the posted SetComplete/flush callbacks.
	for end := time.Now().Add(200 * time.Millisecond); time.Now().Before(end); {
		app.ui.RenderNow(true)
		time.Sleep(2 * time.Millisecond)
	}

	if pendingBashIn(app) != nil {
		t.Fatal("the completed bash frame is still attached to the footer dock")
	}
	if bashComponentIn(app) == nil {
		t.Fatal("the completed bash frame is not in the transcript")
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

// A manual `!` frame is click-to-expand like a tool frame (upstream only wires
// the MouseRegion in tool-execution.ts; bash-execution.ts has no click target).
func TestClickingTheBashFrameTogglesExpansion(t *testing.T) {
	component := NewBashExecutionComponent("dua", nil, false, 1)
	for i := 0; i < 30; i++ {
		component.AppendOutput("entry " + itoa(i) + "\n")
	}
	exit := 0
	component.SetComplete(&exit, false, nil, "")

	collapsed := component.Render(80)
	click := tui.TuiMouseEvent{Type: tui.MouseClick, Button: tui.MouseButtonLeft, X: 2, Y: 3, Width: 80, Height: len(collapsed)}
	if result := component.HandleMouse(click); result == nil || !result.Handled {
		t.Fatalf("click on the bash frame was not handled (render = %d rows)", len(collapsed))
	}
	expanded := component.Render(80)
	if len(expanded) <= len(collapsed) {
		t.Fatalf("click did not expand the frame: %d rows -> %d rows", len(collapsed), len(expanded))
	}
}
