package interactive

import (
	"context"
	"errors"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestAppComposition covers the interactive-mode object graph: construction,
// mounting, focus and the submit/key wiring.
func TestAppComposition(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	app.Init(context.Background())
	if len(app.ui.GetMountedRoots()) == 0 {
		t.Fatal("no mounted roots")
	}
	if app.ui.GetFocusedComponent() != app.defaultEditor {
		t.Fatal("editor not focused")
	}
	if !app.lifecycle.IsInitialized() {
		t.Fatal("lifecycle not marked initialized")
	}

	// A slash command routes through the submit handler into the chat. The
	// session panel is built off the loop and posted back to it (D159), so the
	// test plays the loop until it lands.
	app.submit.HandleSubmit(context.Background(), "/session")
	waitForConditionWithin(t, func() bool {
		app.ui.RenderNow(true)
		return strings.Contains(renderAppChat(app), "Session")
	}, 5*time.Second)

	// The key wiring installs the editor escape/action handlers.
	app.key.SetupKeyHandlers(func() int64 { return time.Now().UnixMilli() })
	if app.defaultEditor.OnEscape == nil {
		t.Fatal("escape handler not installed")
	}
	if len(app.defaultEditor.ActionHandlers) == 0 {
		t.Fatal("action handlers not installed")
	}
}

// TestAppEndToEndLoop drives the app's main input loop end to end: an input
// queued from the editor reaches the session and the transcript.
func TestAppEndToEndLoop(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		app.Run(ctx)
	}()

	// These deadlines must cover a loaded 2-core runner: the whole package took
	// 63 s there and init missed 6 s (CI 36266574631), and 95 s on a loaded
	// 4-core box under the full race gate. The condition is still required; only
	// the poll window scales with the machine.
	waitForConditionWithin(t, func() bool { return app.lifecycle.IsInitialized() }, 45*time.Second)
	// Wait for the run loop to reach its first beat before queueing, so the input
	// is read from the loop's select rather than racing startup.
	waitForConditionWithin(t, func() bool { return app.runner != nil && app.runner.LoopBeats() > 0 }, 45*time.Second)
	app.startup.QueueUserInput("hello from the smoke test")

	// The loop forwards the input to the session; with no model the prompt
	// errors, but the user message is recorded before the model call.
	waitForConditionWithin(t, func() bool {
		var sawUser, sawAssistant bool
		for _, message := range app.session.Messages() {
			switch typed := message.(type) {
			case *ai.UserMessage:
				if ai.ContentText(typed.Content, "") == "hello from the smoke test" {
					sawUser = true
				}
			case *ai.AssistantMessage:
				if ai.ContentText(ai.StringOrBlocks{Blocks: typed.Content}, "") == "ack" {
					sawAssistant = true
				}
			}
		}
		return sawUser && sawAssistant
	}, 60*time.Second)

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("run loop did not exit after cancellation")
	}
}

func newTestApp(t *testing.T) (*App, func()) {
	t.Helper()
	return newTestAppWithPersist(t, false)
}

// newTestAppPersisted is newTestApp with a session file, for commands that need
// one (handoff).
func newTestAppPersisted(t *testing.T) (*App, func()) {
	t.Helper()
	return newTestAppWithPersist(t, true)
}

func newTestAppWithPersist(t *testing.T, persist bool) (*App, func()) {
	t.Helper()
	return newTestAppConfigured(t, persist, nil)
}

// newTestAppConfigured is newTestAppWithPersist with a hook that adjusts the
// settings and app options before NewApp composes the object graph.
func newTestAppConfigured(t *testing.T, persist bool, configure func(*coding.SettingsManager, *AppOptions)) (*App, func()) {
	t.Helper()
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	dark := "dark"
	InitTheme(dark, false)

	appKeybindings := NewAppKeybindingsManager(nil, "")
	previous := tui.GetKeybindings()
	tui.SetKeybindings(appKeybindings.KeybindingsManager)

	dir := t.TempDir()
	refresh := false
	credentials := ai.NewInMemoryCredentialStore()
	if _, err := credentials.Modify(context.Background(), "anthropic", func(*ai.Credential) (*ai.Credential, error) {
		return &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "test-key"}}, nil
	}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	runtime, err := coding.CreateModelRuntime(coding.CreateModelRuntimeOptions{
		Credentials: credentials, RefreshOnCreate: &refresh,
	})
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	anthropicModels := runtime.GetModels("anthropic")
	if len(anthropicModels) == 0 {
		t.Fatal("no anthropic models in the catalog")
	}
	settings := coding.NewInMemorySettingsManager(nil, coding.SettingsManagerCreateOptions{})
	sessions := coding.NewSessionManager(dir, &coding.SessionManagerOptions{Persist: &persist})
	fauxModel := anthropicModels[0]
	created, err := coding.CreateAgentSession(context.Background(), &coding.CreateAgentSessionOptions{
		Cwd: dir, AgentDir: dir, Model: fauxModel, StreamFn: ackStreamFn,
		SessionManager: sessions, ModelRuntime: runtime, SettingsManager: settings,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	appOptions := AppOptions{
		Cwd:                 dir,
		AgentDir:            dir,
		Terminal:            &fakeRendererTerminal{width: 80, height: 24},
		TuiMode:             "regular",
		Version:             "1.0.0",
		AppName:             "pi",
		QuietStartup:        coding.QuietStartupSetting{Enabled: true},
		Settings:            settings,
		Session:             created.Session,
		Runtime:             runtime,
		SessionMgr:          sessions,
		Keybindings:         appKeybindings,
		InitialThemeSetting: &dark,
		Exit:                func(int) {},
		RegisterSignal:      func(os.Signal, func()) func() { return func() {} },
	}
	if configure != nil {
		configure(settings, &appOptions)
	}
	app := NewApp(appOptions)

	// Disable the render timer so the test's direct component access cannot race
	// with a background render. app.UI is a TuiReference (D105), so resolve the
	// concrete renderer instead of type-asserting the reference itself.
	disableAutoRenderForTest(app)

	cleanup := func() {
		app.lifecycle.UnregisterSignalHandlers()
		tui.SetKeybindings(previous)
	}
	// Close stops the app's workers (the footer's git watcher, the off-loop
	// queues) so a test cannot leave them running (internal/leakcheck).
	t.Cleanup(app.Close)
	return app, cleanup
}

// disableAutoRenderForTest turns off the render timer on the app's current
// renderer (TuiReference-aware: the D105 forwarding reference hides the
// concrete MainScreen from a plain type assertion).
func disableAutoRenderForTest(app *App) {
	if app == nil {
		return
	}
	if screen, ok := app.currentRenderer().(*tui.MainScreen); ok {
		screen.DisableAutoRender()
	}
}

// waitForConditionWithin polls until the condition holds or the deadline
// passes (a longer deadline than the shared helper for the end-to-end loop,
// which runs under heavy suite load).
func waitForConditionWithin(t *testing.T, condition func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	// A timeout is either genuine machine starvation or a stalled loop; the
	// dump makes the difference visible in CI output.
	buffer := make([]byte, 1<<20)
	t.Logf("goroutines at timeout:\n%s", buffer[:runtime.Stack(buffer, true)])
	t.Fatal("condition not met before timeout")
}

// waitForRenderQuiet waits until no paint has landed for quiet, then returns.
// Startup paints are not bounded by a fixed sleep: under load they can arrive
// later than any guess, and a test that measures paint counts afterwards would
// count them.
func waitForRenderQuiet(t *testing.T, ui tui.TUI, quiet time.Duration, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := ui.RenderCount()
	stableSince := time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		current := ui.RenderCount()
		if current != last {
			last = current
			stableSince = time.Now()
			continue
		}
		if time.Since(stableSince) >= quiet {
			return
		}
	}
	t.Fatalf("paints never settled for %s", quiet)
}

// renderAppChat renders the chat without racing the app's loop: when the loop
// is running it executes the render on the loop goroutine (the containers are
// loop-owned since the container lock was removed); otherwise nothing else
// paints and the test renders directly.
func renderAppChat(app *App) string {
	done := make(chan []string, 1)
	app.ui.Post(func() {
		lines := app.chat.Render(80)
		select {
		case done <- lines:
		default:
		}
	})
	select {
	case lines := <-done:
		return coding.StripAnsi(strings.Join(lines, "\n"))
	case <-time.After(200 * time.Millisecond):
		// No loop consumer: render from the test goroutine.
		lines := app.chat.Render(80)
		return coding.StripAnsi(strings.Join(lines, "\n"))
	}
}

func (h *inputDeadlineHost) NextInputFlushDeadline() (time.Time, bool) {
	return h.buffer.PendingTimeout()
}

func (h *inputDeadlineHost) FlushPendingInput() {
	h.input = append(h.input, h.buffer.FlushExpired(time.Now())...)
}

// The composed app's resume message is the reference's two-line shape.
func TestAppResumeMessageIsTwoLines(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	message := app.lifecycle.options.FormatResumeMessage("pier --session abc")
	if !strings.HasSuffix(message, "\npier --session abc") {
		t.Fatalf("resume message = %q, want the command on its own line", message)
	}
}

// pi v1.5.2 (b4afdddf) extensions/mcp/cli.ts:159-164 reads an `auth.provider` token
// through the model runtime and catches a missing or unrefreshable credential,
// returning undefined so the request goes out without an Authorization header and the
// server's 401 reports that sign-in is needed. The port returned the error, which failed
// the MCP connection before any request was sent.
func TestProviderTokenFailureSendsNoToken(t *testing.T) {
	failing := providerTokenStub{resolution: nil, err: errors.New("no credential")}
	if got, err := providerTokenFromRuntime(context.Background(), failing, "openai"); err != nil || got != "" {
		t.Fatalf("failed credential = %q err = %v, want an empty token and no error", got, err)
	}
	if got, err := providerTokenFromRuntime(context.Background(), providerTokenStub{}, "openai"); err != nil || got != "" {
		t.Fatalf("nil resolution = %q err = %v, want an empty token and no error", got, err)
	}
	ok := providerTokenStub{resolution: &ai.AuthResult{Auth: ai.ModelAuth{APIKey: "sk-1"}}}
	if got, err := providerTokenFromRuntime(context.Background(), ok, "openai"); err != nil || got != "sk-1" {
		t.Fatalf("resolved credential = %q err = %v, want sk-1", got, err)
	}
}

type providerTokenStub struct {
	resolution *ai.AuthResult
	err        error
}

func (s providerTokenStub) GetAuth(string, *coding.ModelRuntimeAuthOverrides) (*ai.AuthResult, error) {
	return s.resolution, s.err
}

// ackStreamFn answers every prompt with a done Anthropic-API "ack" message.
func ackStreamFn(model *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	go func() {
		message := &ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: model.Provider, Model: model.ID,
			Content: ai.ContentList{ai.TextContent{Text: "ack"}}, StopReason: ai.StopStop,
		}
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: message})
	}()
	return stream
}
