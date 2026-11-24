package interactive

import (
	"context"
	"errors"
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

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
	"github.com/dat267/pier/tui"
)

// ModelWiring's optional sinks are nil-checked so a headless wiring can call
// them safely (the port's error and render helpers). Inverting the checks calls
// a nil func and panics.
func TestModelWiringGuardsOptionalSinks(t *testing.T) {
	wiring := &ModelWiring{}
	wiring.showWarning("ignored")
	wiring.showError("ignored")
	wiring.requestRender()

	var warnings []string
	wiring.ShowWarning = func(message string) { warnings = append(warnings, message) }
	wiring.showWarning("warned")
	if len(warnings) != 1 || warnings[0] != "warned" {
		t.Fatalf("warnings = %v", warnings)
	}

	renders := 0
	wiring.RequestRender = func() { renders++ }
	wiring.requestRender()
	if renders != 1 {
		t.Fatalf("renders = %d, want the configured callback", renders)
	}
}

// SettingsWiring's optional sinks are nil-checked so a wiring without a UI or
// transcript still applies settings. Inverting a check calls a nil func or a nil
// container and panics.
func TestSettingsWiringGuardsOptionalSinks(t *testing.T) {
	settings := coding.NewInMemorySettingsManager(&coding.Settings{}, coding.SettingsManagerCreateOptions{})
	wiring := &SettingsWiring{Settings: settings}
	wiring.showStatus("ignored")
	wiring.requestRender()

	callbacks := wiring.BuildSettingsCallbacks(func() {}, func() {})
	callbacks.OnMermaidRenderingModeChange("off")
	callbacks.OnHideThinkingBlockChange(true)

	renders := 0
	wiring.RequestRender = func() { renders++ }
	wiring.requestRender()
	if renders != 1 {
		t.Fatalf("renders = %d, want the configured callback", renders)
	}
}

// The production composition validates its required seams once: a forgotten
// assignment must fail at construction, not silently during a session (the
// queue reporters were once dropped, so their statuses vanished).
func TestAppWiringValidation(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	if missing := app.validateWiring(); len(missing) != 0 {
		t.Fatalf("a fully built app reports missing wiring: %v", missing)
	}

	app.events.ShowError = nil
	missing := app.validateWiring()
	if len(missing) != 1 || missing[0] != "events.ShowError" {
		t.Fatalf("validator = %v, want [events.ShowError]", missing)
	}

	app.events.ShowError = func(string) {}
	if missing := app.validateWiring(); len(missing) != 0 {
		t.Fatalf("restoring the seam still reports %v", missing)
	}
}

// A seam audit found func-typed wiring fields that no production code assigned.
// Most were deliberate (test seams, host hooks, D41 vestiges) and are documented
// where they live, one (ConfigureHTTPIdleTimeout) was a dispatcher seam with
// nothing safe to install (D254), and one was a real gap, pinned here.

// The global debug key is the renderer's: upstream matches shift+ctrl+d in
// tui.ts and interactive-mode.ts points ui.onDebug at handleDebugCommand, so the
// key and /debug do the same work. Neither hook was assigned here.
func TestDebugKeyRunsTheDebugCommand(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir) // the debug log path follows the env
	app, cleanup := newTestApp(t)
	defer cleanup()

	screen, ok := tuiConcrete(app.ui).(*tui.MainScreen)
	if !ok {
		t.Fatalf("renderer = %T", tuiConcrete(app.ui))
	}
	if screen.MatchesDebugKey == nil || screen.OnDebug == nil {
		t.Fatal("the renderer's debug key is not wired")
	}
	if screen.MatchesDebugKey("x") {
		t.Error("MatchesDebugKey accepted a plain character")
	}

	// The callback is the same work /debug does: a debug log in the agent dir.
	screen.OnDebug()
	if _, err := os.Stat(filepath.Join(agentDir, coding.AppName+"-debug.log")); err != nil {
		t.Fatalf("the debug key wrote no debug log: %v", err)
	}
}

// pi v1.5.0, model-search.ts getDistinctModelName: isCovered tests
// `word.length > 1`, JavaScript string length, so a single astral letter counts
// as two and can be covered by a compact id. Counting runes made such a name
// distinct when the reference hides it.
func TestDistinctModelNameCountsUTF16Units(t *testing.T) {
	if name, ok := GetDistinctModelName("a𝔘b", "𝔘"); ok {
		t.Fatalf("name %q reported distinct, want the astral letter covered by the compact id", name)
	}
	if name, ok := GetDistinctModelName("a𝔘b", "x"); !ok || name != "x" {
		t.Fatalf("name %q ok=%v, want a distinct name", name, ok)
	}
}

// Reference: pi v1.4.0 (4a080252), oauth-selector.ts filterProviders includes
// provider.method.name. The real /login options carry *ai.OAuthAuth methods.
func TestOAuthSelectorSearchesAuthMethodName(t *testing.T) {
	wiring, _ := newAuthTestWiring(t)
	providers := wiring.GetLoginProviderOptions("")
	for _, tc := range []struct {
		query string
		match bool
	}{
		{"ChatGPT", true},
		{"OpenAI", true},
		{"not-a-real-login-method", false},
	} {
		t.Run(tc.query, func(t *testing.T) {
			selected := ""
			selector := NewOAuthSelectorComponent("login", providers,
				func(id, authType string) { selected = id }, nil, tc.query)
			selector.HandleInput("\r")
			if (selected != "") != tc.match {
				rendered := tui.StripTerminalSequences(strings.Join(selector.Render(80), "\n"))
				t.Fatalf("query %q selected %q, want match=%v; rendered:\n%s", tc.query, selected, tc.match, rendered)
			}
		})
	}
}

// pi 4a080252 components/scoped-models-selector.ts applies its refresh result
// on the JS event loop, so dismissal and the in-flight refresh never touch the
// same state concurrently. Pier's refresh worker reads the dismissal flag while
// the owner writes it; this exercises that handoff under -race.
func TestScopedModelsRefreshObservesDismissalSafely(t *testing.T) {
	models := []*ai.Model{{ID: "a", Provider: "p"}}
	finished := make(chan struct{})
	runtime := &modelTestRuntime{models: models, refresh: func() (ai.ModelsRefreshResult, error) {
		close(finished)
		return ai.ModelsRefreshResult{}, errors.New("offline")
	}}
	wiring, _, _ := newModelTestWiring(t, runtime)
	wiring.ShowModelsSelector(context.Background())
	<-finished
	// The refresh has settled, so the worker is about to re-read the dismissal
	// flag while the owner sets it. No channel orders those two accesses, so
	// -race reports the unsynchronized read.
	wiring.Slot.DisposeActiveSelector()
	for range 2 {
		wiring.UI.(*tui.MainScreen).RenderNow(true)
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

// A search query moves the selection to the top row, the best match: after
// scrolling down in /login, typing a query used to leave the highlight on a row
// that only happened to survive the clamp, so Enter opened the wrong provider
// (oauth-selector.ts filterProviders, v1.6.0, #12245).
func TestOAuthSelectorSearchSelectsTheBestMatch(t *testing.T) {
	wiring, _ := newAuthTestWiring(t)
	providers := wiring.GetLoginProviderOptions("")
	selector := NewOAuthSelectorComponent("login", providers,
		func(id, authType string) {}, nil, "")
	// Scroll down a few rows, then search.
	for range 5 {
		selector.HandleInput("\x1b[B")
	}
	if selector.selectedIndex == 0 {
		t.Fatal("the test needs a non-zero selection before searching")
	}
	for _, char := range "openai" {
		selector.HandleInput(string(char))
	}
	if got := selector.selectedIndex; got != 0 {
		t.Fatalf("selected index after a query = %d, want the top row", got)
	}
	// Clearing the query keeps the clamp instead of resetting the row.
	selector.HandleInput("\x7f")
	if got := selector.selectedIndex; got != 0 {
		t.Fatalf("selected index after deleting a character = %d, want the clamp", got)
	}
}

// Cursor keys move the search cursor without changing the query, so they must not
// refilter: a refilter moves the selection to the top row, which would throw away
// the cursor position the user is editing (select-list/model-selector cursor
// handling, v1.6.0, #12334).
func TestOAuthSelectorCursorKeysKeepTheSelection(t *testing.T) {
	wiring, _ := newAuthTestWiring(t)
	providers := wiring.GetLoginProviderOptions("")
	selector := NewOAuthSelectorComponent("login", providers,
		func(id, authType string) {}, nil, "")
	for range 3 {
		selector.HandleInput("\x1b[B")
	}
	before := selector.selectedIndex
	if before == 0 {
		t.Fatal("the test needs a non-zero selection")
	}
	selector.HandleInput("\x1b[D")
	selector.HandleInput("\x1b[C")
	if after := selector.selectedIndex; after != before {
		t.Fatalf("selection = %d after cursor keys, want %d kept", after, before)
	}
}
