package interactive

import (
	"context"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
	"github.com/dat267/pier/tui"
	"go/ast"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
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
	if _, err := credentials.Modify("anthropic", func(*ai.Credential) (*ai.Credential, error) {
		return &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "test-key"}}, nil
	}, context.Background()); err != nil {
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
	streamFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
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
	created, err := coding.CreateAgentSession(context.Background(), &coding.CreateAgentSessionOptions{
		Cwd: dir, AgentDir: dir, Model: fauxModel, StreamFn: streamFn,
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

// TestAppWiringCompleteness guards the composition root's failure mode: a
// wiring field that NewApp silently never assigns. /debug (WriteDebugLog) and
// right-click paste both shipped unwired this way. Each wiring now owns its
// constructor, so the required hooks are asserted here after NewApp.
func TestAppWiringCompleteness(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	required := []struct {
		name string
		set  bool
	}{
		{"Commands.WriteDebugLog", app.commands.WriteDebugLog != nil},
		{"Commands.CopyToClipboard", app.commands.CopyToClipboard != nil},
		{"Commands.ReloadNow", app.commands.ReloadNow != nil},
		{"Commands.ApplyReloadedSettings", app.commands.ApplyReloadedSettings != nil},
		{"Commands.Exporters", app.commands.Exporters != nil},
		{"Commands.MarkdownTheme", app.commands.MarkdownTheme != nil},
		{"Commands.RunDetached", app.commands.RunDetached != nil},
		{"Autocomplete.Skills", app.autocomplete.Skills != nil},
		{"Autocomplete.LoginProviders", app.autocomplete.LoginProviders != nil},
		{"Runner.ShowLoadedResources", app.runner.ShowLoadedResources != nil},
		{"Runner.OnSignal", app.runner.OnSignal != nil},
		{"Runner.Prompt", app.runner.Prompt != nil},
		{"Runner.RefreshModelCatalogs", app.runner.RefreshModelCatalogs != nil},
		{"Runner.CheckVersion", app.runner.CheckVersion != nil},
		{"Runner.TakeCrash", app.runner.TakeCrash != nil},
		{"Runner.ShowError", app.runner.ShowError != nil},
		{"Runner.RequestRender", app.runner.RequestRender != nil},
		{"Runner.SetupKeyHandlers", app.runner.SetupKeyHandlers != nil},
		{"Runner.SetupSubmitHandler", app.runner.SetupSubmitHandler != nil},
		{"Key.OnToolsExpand", app.key.OnToolsExpand != nil},
		{"Key.OnFollowUp", app.key.OnFollowUp != nil},
		{"Key.OnDequeue", app.key.OnDequeue != nil},
		{"Key.OnPasteImage", app.key.OnPasteImage != nil},
		{"Key.OnModelSelect", app.key.OnModelSelect != nil},
		{"Key.OnSessionTree", app.key.OnSessionTree != nil},
		{"Key.OnExit", app.key.OnExit != nil},
		{"Submit.Handlers.HandleDebugCommand", app.submit.Handlers.HandleDebugCommand != nil},
		{"Submit.Handlers.HandleReloadCommand", app.submit.Handlers.HandleReloadCommand != nil},
		{"Submit.Handlers.HandleCompactCommand", app.submit.Handlers.HandleCompactCommand != nil},
		{"Submit.Handlers.HandleExportCommand", app.submit.Handlers.HandleExportCommand != nil},
		{"Submit.Handlers.HandleHotkeysCommand", app.submit.Handlers.HandleHotkeysCommand != nil},
		{"Submit.Handlers.HandleChangelogCommand", app.submit.Handlers.HandleChangelogCommand != nil},
		{"Submit.Handlers.ShowTrustSelector", app.submit.Handlers.ShowTrustSelector != nil},
		{"Submit.Handlers.Shutdown", app.submit.Handlers.Shutdown != nil},
		{"Trust.Stop", app.trust.Stop != nil},
		{"Startup.RenderInitialMessages", app.startup.RenderInitialMessages != nil},
		{"Transcript.RenderProjectTrustWarning", app.transcript.RenderProjectTrustWarning != nil},
		{"Startup.ShowError", app.startup.ShowError != nil},
		{"Startup.ShowStatus", app.startup.ShowStatus != nil},
		{"Startup.RequestRender", app.startup.RequestRender != nil},
		{"Selectors.ShowError", app.selectors.ShowError != nil},
		{"Selectors.RebuildChat", app.selectors.RebuildChat != nil},
		{"Selectors.SetNavigatedEditorText", app.selectors.SetNavigatedEditorText != nil},
		{"Selectors.FlushCompactionQueue", app.selectors.FlushCompactionQueue != nil},
		{"SettingsW.RequestRender", app.settingsW.RequestRender != nil},
		{"Models.ShowError", app.models.ShowError != nil},
		{"Sessions.Shutdown", app.sessions.Shutdown != nil},
		{"Auth.ShowError", app.auth.ShowError != nil},
	}
	for _, check := range required {
		if !check.set {
			t.Errorf("%s is not wired", check.name)
		}
	}
}

// The navigated point's message text lands in the editor only while the editor
// is empty, upstream's `result.editorText && !this.editor.getText().trim()`:
// re-editing a draft must not be overwritten by the tree navigation.
func TestNavigatedEditorTextKeepsDrafts(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	app.selectors.SetNavigatedEditorText("from the tree")
	if got := app.defaultEditor.GetText(); got != "from the tree" {
		t.Errorf("editor text = %q", got)
	}

	app.defaultEditor.SetText("my draft")
	app.selectors.SetNavigatedEditorText("another point")
	if got := app.defaultEditor.GetText(); got != "my draft" {
		t.Errorf("a draft was overwritten: %q", got)
	}

	// Whitespace-only counts as empty, as upstream's trim() does.
	app.defaultEditor.SetText("   ")
	app.selectors.SetNavigatedEditorText("third point")
	if got := app.defaultEditor.GetText(); got != "third point" {
		t.Errorf("whitespace-only editor = %q", got)
	}
}

// Upstream draws the untrusted-project warning from renderInitialMessages
// (interactive-mode.ts:4062) — the function the tree navigation calls to rebuild
// the transcript, so the warning comes back with it. The port's
// RenderProjectTrustWarningIfNeeded was only ever called by tests, so the
// warning never appeared on screen at all.
func TestRenderInitialMessagesWarnsAboutUntrustedProject(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	cwd := app.sessionMgr.GetCwd()
	configDir := filepath.Join(cwd, coding.ConfigDirName, "skills")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// SettingsManagerCreateOptions.ProjectTrusted defaults to true.
	app.settings.SetProjectTrusted(false)
	if app.settings.IsProjectTrusted() {
		t.Fatal("the test project should be untrusted")
	}
	if !coding.HasTrustRequiringProjectResources(cwd) {
		t.Fatalf("expected %s to require trust", configDir)
	}

	app.chat.Clear()
	app.transcript.RenderInitialMessages()
	rendered := strings.Join(renderChat(t, app.chat), "\n")
	if !strings.Contains(rendered, "This project is not trusted") {
		t.Errorf("the trust warning is missing from the transcript:\n%s", rendered)
	}
}

// TestAppChatSkipsUnchangedChildren pins the wiring for the scroll fast path:
// the transcript's chat must be the container that skips re-rendering
// unchanged versioned children.
func TestAppChatSkipsUnchangedChildren(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	if !app.chat.SkipUnchangedChildren {
		t.Fatal("app.chat must skip unchanged versioned children")
	}
}

// #9311: RenderSessionEntries is the funnel every transcript rebuild goes through, so
// it drops the fullscreen selection before the transcript it points into is replaced.
func TestRenderSessionEntriesDropsTheFullscreenSelection(t *testing.T) {
	transcript := NewTranscriptRenderer(&tui.Container{}, nil, nil, nil, nil)
	calls := 0
	transcript.ResetTextSelection = func() { calls++ }
	transcript.RenderSessionEntries(nil, false, false)
	if calls != 1 {
		t.Fatalf("selection reset called %d times, want 1", calls)
	}
}

// The app installs the reset on its transcript renderer, and resolves the renderer when
// a rebuild happens, so a mode change is followed instead of captured.
func TestAppInstallsTheTranscriptSelectionReset(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	if app.transcript == nil || app.transcript.ResetTextSelection == nil {
		t.Fatal("the app did not install the transcript selection reset")
	}
	// An inline renderer tracks no selection, so this has to be a no-op rather than a
	// panic or an assertion failure.
	app.transcript.ResetTextSelection()
}

// Upstream sets the terminal title at init (and after a session switch).
func TestAppInitSetsTerminalTitle(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	terminal := &titleTerminal{width: 80, height: 24}
	app.lifecycle.options.Terminal = terminal
	app.events.Init()
	if len(terminal.titles) == 0 {
		t.Fatal("init did not set the terminal title")
	}
}

// Upstream shows the new-changelog notice at startup (showStartupNoticesIfNeeded).
func TestAppShowsStartupChangelogNotice(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	app.settings.SetLastChangelogVersion("0.0.1")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte("## [9.9.9]\n\n- a new thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	coding.SetPackageDir(dir)
	t.Cleanup(func() { coding.SetPackageDir("") })

	// The changelog notice only shows for a session with no messages.
	app.session.Agent.SetMessages(nil)
	app.chat.Clear()
	app.startupNoticesShown = false
	app.showStartupNoticesIfNeeded()
	lines := coding.StripAnsi(strings.Join(app.chat.Render(120), "\n"))
	if !strings.Contains(lines, "What's New") || !strings.Contains(lines, "a new thing") {
		t.Fatalf("chat = %q", lines)
	}
}

// Toggling thinking-block visibility re-renders the existing assistant messages
// (upstream updateThinkingBlockVisibility).
func TestUpdateThinkingBlockVisibilityUpdatesComponents(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	markdownTheme := GetMarkdownTheme()
	message := &ai.AssistantMessage{Content: ai.ContentList{ai.ThinkingContent{Thinking: "hmm"}}}
	component := NewAssistantMessageComponent(message, false, &markdownTheme, "", 0, nil)
	app.chat.AddChild(component)
	app.updateThinkingBlockVisibility(true)
	if !component.hideThinkingBlock {
		t.Fatal("the rendered assistant message did not hide its thinking block")
	}
}

// ctrl+n (app.session.new) goes through handleClearCommand, which clears the
// status indicator first (upstream onAction("app.session.new", handleClearCommand)).
func TestKeyWiringSessionNewClearsStatus(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	app.uiState.ShowStatusIndicator(NewCompactionStatusIndicator(app.ui, "manual", nil))
	if app.uiState.ActiveStatusIndicator == nil {
		t.Fatal("setup: no active indicator")
	}
	app.key.OnSessionNew()
	if app.uiState.ActiveStatusIndicator != nil {
		t.Fatal("OnSessionNew did not clear the status indicator")
	}
}

// The external editor receives the expanded prompt, not the paste marker
// (upstream `editor.getExpandedText?.() ?? editor.getText()`).
func TestExternalEditorUsesExpandedText(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	pasted := strings.Repeat("x", 1200)
	app.defaultEditor.HandleInput("\x1b[200~" + pasted + "\x1b[201~")
	if got := app.defaultEditor.GetText(); got == pasted {
		t.Fatal("setup: the paste was not collapsed to a marker")
	}
	expanded := app.defaultEditor.GetExpandedText()
	captured := ""
	previous := externalEditorRunner
	externalEditorRunner = func(command *exec.Cmd) error {
		if len(command.Args) == 0 {
			return nil
		}
		data, err := os.ReadFile(command.Args[len(command.Args)-1])
		if err != nil {
			return err
		}
		captured = string(data)
		return nil
	}
	t.Cleanup(func() { externalEditorRunner = previous })
	app.key.OnExternalEditor()
	if captured != expanded {
		t.Fatalf("external editor content = %q; want the expanded %q", captured, expanded)
	}
}

// app.message.copy is bound (upstream onAction("app.message.copy", handleCopyCommand)).
func TestKeyWiringCopyBound(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	if app.key.OnCopy == nil {
		t.Fatal("app.message.copy has no handler")
	}
}

// TestKeybindingMigrationKeepsTheFileOnFailure pins A507 for keybindings.json:
// the migration wrote the file with os.WriteFile, so a failed migration could
// leave the user's bindings truncated.
func TestKeybindingMigrationKeepsTheFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	path := filepath.Join(dir, "keybindings.json")

	// A legacy name the migration rewrites.
	previous := []byte("{\n  \"submit\": \"ctrl+enter\"\n}\n")
	if err := os.WriteFile(path, previous, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if MigrateKeybindingsFile(path) {
		t.Fatalf("a migration that cannot publish reported success")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("the failed migration removed keybindings.json: %v", readErr)
	}
	if string(got) != string(previous) {
		t.Errorf("the failed migration left %q, want the previous bindings", got)
	}
}

// D185: /reload and session replacement re-read mcp.json (upstream
// session_start runs ensureDiscoveryActive again), so the warning for tools
// pier cannot expose comes back with the reload's MCP messages, after the
// config's own load errors, and only while such a server is configured.
func TestReloadMCPWarnsAboutUnreachableTools(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	transport := func(entry coding.McpServerEntry, _ string, _ mcp.AuthProvider) (mcp.Transport, error) {
		return newMCPReloadFakeServer(t, entry.Name, []protocol.Tool{{Name: "echo"}}), nil
	}
	app.mcpOptionsOverride = func(loaded coding.LoadedMcpConfig) coding.McpManagerOptions {
		return coding.McpManagerOptions{Config: loaded, Cwd: app.sessionMgr.GetCwd(), CreateTransport: transport}
	}
	defer func() {
		if app.mcpManager != nil {
			_ = app.mcpManager.Close(context.Background())
		}
	}()

	writeMCPReloadConfig(t, app.mcpAgentDir, `{"mcpServers":{"gopls":{"command":"unused"}}}`)
	messages := app.reloadMCP(context.Background(), app.sessionMgr.GetCwd())
	if !slices.ContainsFunc(messages, func(message string) bool {
		return strings.HasPrefix(message, `MCP server "gopls" has tools with codemode`)
	}) {
		t.Fatalf("reload messages = %q, want the unreachable-tools warning", messages)
	}

	writeMCPReloadConfig(t, app.mcpAgentDir, `{"mcpServers":{"gopls":{"command":"unused","exposure":"direct"}}}`)
	if messages := app.reloadMCP(context.Background(), app.sessionMgr.GetCwd()); len(messages) != 0 {
		t.Fatalf("direct-exposure reload messages = %q, want none", messages)
	}
}

// TestAppExportedSurface pins App's cross-package surface. cmd is the only
// consumer outside this package, and it calls Run and StopMode on the app it
// built with NewApp, plus Init and Close. Everything else App offers is
// in-package state: the wirings and the interactive tests are all in this
// package, so an exported member buys nothing and misleads a reader into
// treating it as API.
//
// This file exists because app.go is the most frequently changed file in the
// repository — and its surface was the largest. The guard is the point: the
// surface cannot quietly grow back one field at a time.
func TestAppExportedSurface(t *testing.T) {
	allowed := map[string]bool{"Init": true, "Run": true, "Close": true, "StopMode": true}

	// The pointer type for methods: every App method has a pointer receiver, and
	// the value type's method set is empty here — a guard that cannot fail.
	methodSet := reflect.TypeFor[*App]()
	for method := range methodSet.Methods() {
		if name := method.Name; !allowed[name] {
			t.Errorf("App.%s is exported; the cross-package surface is exactly Init, Run, Close and StopMode", name)
		}
	}
	appType := reflect.TypeFor[App]()
	for field := range appType.Fields() {
		// An embedded type's name is its type's, not App's own surface.
		if !field.Anonymous && ast.IsExported(field.Name) {
			t.Errorf("App.%s is exported; App's fields are in-package state", field.Name)
		}
	}
}

// D371: a failed manual Go copy must not leave its newly created session file.
// Reference: pi v1.5.0 (f9a26365), coding-agent/src/core/agent-session-runtime.ts
// importFromJsonl delegates to Node's copyFileSync/COPYFILE_EXCL. This pins the
// Go error path, not a claim that Node rejects this directory input.
func TestImportCopyFailureDoesNotLeaveAPartialSession(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "fresh destination"
		if existing {
			name = "existing name preserved"
		}
		t.Run(name, func(t *testing.T) {
			app, cleanup := newTestApp(t)
			defer cleanup()
			sessionDir := withSessionDir(t, app)
			previousID := app.sessionMgr.GetSessionID()
			source := filepath.Join(t.TempDir(), "broken.jsonl")
			if err := os.Mkdir(source, 0o700); err != nil {
				t.Fatal(err)
			}
			original := filepath.Join(sessionDir, filepath.Base(source))
			if existing {
				if err := os.WriteFile(original, []byte("keep this session"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			newCommandWiring(app).HandleImportCommand(context.Background(), "/import "+source)
			answerConfirm(t, app, 0)
			if !transcriptContains(app, "Failed to import session:") {
				t.Fatal("copy failure was not reported")
			}
			if app.sessionMgr.GetSessionID() != previousID {
				t.Fatal("failed copy replaced the active session")
			}
			entries, err := os.ReadDir(sessionDir)
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if existing {
				want = 1
				data, err := os.ReadFile(original)
				if err != nil || string(data) != "keep this session" {
					t.Fatalf("existing session changed: %q, err=%v", data, err)
				}
			}
			if len(entries) != want {
				t.Fatalf("session files after failed copy = %v, want %d existing files only", entries, want)
			}
		})
	}
}

type inputDeadlineHost struct {
	fakeLoopHost
	buffer *tui.StdinBuffer
	input  []string
}

func (h *inputDeadlineHost) NextInputFlushDeadline() (time.Time, bool) {
	return h.buffer.PendingTimeout()
}

func (h *inputDeadlineHost) FlushPendingInput() {
	h.input = append(h.input, h.buffer.FlushExpired(time.Now())...)
}

// pi v1.4.0 packages/tui/src/stdin-buffer.ts gives a lone Escape its own
// timeout. D143/D145 move that deadline onto the Go UI loop; an unrelated
// idle beat must not discard it just because no component animates.
func TestSchedulePreservesPendingEscapeAcrossIdleBeats(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		host := &inputDeadlineHost{buffer: tui.NewStdinBuffer(tui.StdinBufferOptions{EscapeTimeout: 10})}
		schedule := newLoopSchedule(host, func() {})
		defer schedule.close()
		host.buffer.ProcessString("\x1b")
		if schedule.arm() == nil {
			t.Fatal("Escape did not arm its input deadline")
		}
		// A session/render/resize event can wake the idle loop before Escape's
		// deadline. This is a second scheduling decision at the same fake time.
		wake := schedule.arm()
		if wake == nil {
			t.Fatal("idle beat discarded a still-pending Escape deadline")
		}
		select {
		case <-wake:
			schedule.animationFired()
			schedule.flushExpiredInput()
		case <-time.After(time.Second):
			t.Fatal("Escape deadline did not wake the loop")
		}
		if !reflect.DeepEqual(host.input, []string{"\x1b"}) {
			t.Fatalf("flushed input = %q, want one Escape", host.input)
		}
	})
}

// fdExecutableName is the fixture's file name. exec.LookPath only finds an
// executable extension on Windows (PATHEXT) and returns the path it resolved,
// so the fixture and the expectation both carry ".exe" there.
func fdExecutableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// writeExecutable drops a runnable file so exec.LookPath can find it.
func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// fd is the preferred binary; fdfind is Debian's name for the same thing, and
// accepting it is the difference between @-file completion working and being
// silently unavailable there.
func TestResolveAutocompleteFdPath(t *testing.T) {
	t.Run("fdfind alone", func(t *testing.T) {
		dir := t.TempDir()
		want := filepath.Join(dir, fdExecutableName("fdfind"))
		writeExecutable(t, want)
		t.Setenv("PATH", dir)
		if got := ResolveAutocompleteFdPath(); got != want {
			t.Errorf("resolved %q, want the fdfind fallback", got)
		}
	})

	t.Run("fd wins", func(t *testing.T) {
		dir := t.TempDir()
		want := filepath.Join(dir, fdExecutableName("fd"))
		writeExecutable(t, want)
		writeExecutable(t, filepath.Join(dir, fdExecutableName("fdfind")))
		t.Setenv("PATH", dir)
		if got := ResolveAutocompleteFdPath(); got != want {
			t.Errorf("resolved %q, want fd to take precedence", got)
		}
	})

	t.Run("neither", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if got := ResolveAutocompleteFdPath(); got != "" {
			t.Errorf("resolved %q, want none", got)
		}
	})
}
