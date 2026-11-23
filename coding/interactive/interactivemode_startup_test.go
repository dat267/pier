package interactive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// startupTestSession implements StartupSession.
type startupTestSession struct {
	messages []ai.Message
	scoped   []coding.ScopedModel
	runtime  *coding.ModelRuntime
	model    *ai.Model
}

func (s *startupTestSession) Messages() []ai.Message                    { return s.messages }
func (s *startupTestSession) ScopedModels() []coding.ScopedModel        { return s.scoped }
func (s *startupTestSession) ModelRuntime() *coding.ModelRuntime        { return s.runtime }
func (s *startupTestSession) Model() *ai.Model                          { return s.model }
func (s *startupTestSession) GetToolDefinition(string) *agent.AgentTool { return nil }

func newStartupTestWiring(t *testing.T) (*StartupWiring, *coding.SettingsManager) {
	t.Helper()
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	runtime, err := coding.CreateModelRuntime(coding.CreateModelRuntimeOptions{
		Credentials: ai.NewInMemoryCredentialStore(), DisableModelsJSON: true,
	})
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	settings := coding.NewInMemorySettingsManager(nil, coding.SettingsManagerCreateOptions{})
	session := &startupTestSession{runtime: runtime}
	wiring := &StartupWiring{
		Session:         session,
		Settings:        settings,
		Version:         "1.2.3",
		Chat:            &tui.Container{},
		PendingMessages: &tui.Container{},
		LoadedResources: &tui.Container{},
	}
	return wiring, settings
}

// TestStartupUserInput covers the submission channel: buffered delivery,
// closed-queue shutdown and context cancellation.
func TestStartupUserInput(t *testing.T) {
	wiring, _ := newStartupTestWiring(t)

	// A queued input is returned immediately.
	wiring.QueueUserInput("first")
	value, ok := wiring.GetUserInput(context.Background())
	if !ok || value != "first" {
		t.Fatalf("value = %q, ok = %v", value, ok)
	}

	// A later input is buffered and received by the next read.
	wiring.QueueUserInput("second")
	select {
	case value := <-wiring.Inputs():
		if value != "second" {
			t.Fatalf("value = %q", value)
		}
	case <-time.After(time.Second):
		t.Fatal("input not delivered")
	}

	// A cancelled context returns ok=false.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := wiring.GetUserInput(ctx); ok {
		t.Fatal("cancelled context returned an input")
	}
}

// TestStartupRebuildAndReset covers the chat rebuild helpers.
func TestStartupRebuildAndReset(t *testing.T) {
	wiring, _ := newStartupTestWiring(t)
	manager := coding.NewSessionManager("/tmp/proj", &coding.SessionManagerOptions{Persist: new(false)})
	manager.AppendMessage(ai.Message(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}}))
	wiring.SessionInfo = manager
	wiring.Transcript = NewTranscriptRenderer(wiring.Chat, nil, wiring.Settings, nil, manager)

	wiring.RebuildChatFromMessages()
	if len(wiring.Chat.Children) == 0 {
		t.Fatal("chat not rebuilt")
	}

	// A streaming component and pending tool are cleared by the reset.
	wiring.Transcript.StreamingComponent = &AssistantMessageComponent{}
	wiring.Transcript.pendingTools["t"] = nil
	reset := 0
	wiring.RenderInitialMessages = func() { reset++ }
	wiring.RenderCurrentSessionState()
	if reset != 1 {
		t.Fatalf("reset = %d", reset)
	}
	if wiring.Transcript.StreamingComponent != nil || len(wiring.Transcript.pendingTools) != 0 {
		t.Fatal("streaming state not cleared")
	}
	if len(wiring.LoadedResources.Children) != 0 || len(wiring.PendingMessages.Children) != 0 {
		t.Fatal("containers not cleared")
	}
}

// TestStartupTmuxCheck covers the tmux keyboard warnings.
func TestStartupTmuxCheck(t *testing.T) {
	wiring, _ := newStartupTestWiring(t)

	if warning := wiring.CheckTmuxKeyboardSetup(false); warning != "" {
		t.Fatalf("warning without tmux = %q", warning)
	}
	// A failed query is silent.
	wiring.TmuxShow = func(string) (string, bool) { return "", false }
	if warning := wiring.CheckTmuxKeyboardSetup(true); warning != "" {
		t.Fatalf("warning on query failure = %q", warning)
	}
	// extended-keys off.
	wiring.TmuxShow = func(option string) (string, bool) {
		if option == "extended-keys" {
			return "off", true
		}
		return "", true
	}
	if warning := wiring.CheckTmuxKeyboardSetup(true); !strings.Contains(warning, "extended-keys is off") {
		t.Fatalf("warning = %q", warning)
	}
	// xterm format.
	wiring.TmuxShow = func(option string) (string, bool) {
		if option == "extended-keys" {
			return "on", true
		}
		return "xterm", true
	}
	if warning := wiring.CheckTmuxKeyboardSetup(true); !strings.Contains(warning, "extended-keys-format is xterm") {
		t.Fatalf("warning = %q", warning)
	}
	// csi-u is fine.
	wiring.TmuxShow = func(option string) (string, bool) {
		if option == "extended-keys" {
			return "always", true
		}
		return "csi-u", true
	}
	if warning := wiring.CheckTmuxKeyboardSetup(true); warning != "" {
		t.Fatalf("warning = %q", warning)
	}
}

// TestStartupChangelog covers the changelog display logic.
func TestStartupChangelog(t *testing.T) {
	wiring, settings := newStartupTestWiring(t)
	dir := t.TempDir()
	coding.SetPackageDir(dir)
	defer coding.SetPackageDir("")
	changelog := "## [1.2.3] - 2026-01-01\n\n- New thing [x](docs/a.md)\n\n## [1.2.2] - 2025-12-01\n\n- Old thing\n"
	if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte(changelog), 0o644); err != nil {
		t.Fatalf("write changelog: %v", err)
	}

	// A resumed session never shows the changelog.
	wiring.Session.(*startupTestSession).messages = []ai.Message{&ai.UserMessage{}}
	if got := wiring.GetChangelogForDisplay(); got != "" {
		t.Fatalf("resumed changelog = %q", got)
	}
	wiring.Session.(*startupTestSession).messages = nil

	// A fresh install records the version (the install telemetry ping is not sent:
	// the port has no telemetry backend).
	if got := wiring.GetChangelogForDisplay(); got != "" {
		t.Fatalf("fresh changelog = %q", got)
	}
	if version := settings.GetLastChangelogVersion(); version == nil || *version != "1.2.3" {
		t.Fatalf("version = %v", version)
	}

	// With an older last version the new entries render with normalized links.
	settings.SetLastChangelogVersion("1.2.2")
	got := wiring.GetChangelogForDisplay()
	if !strings.Contains(got, "New thing") || strings.Contains(got, "Old thing") {
		t.Fatalf("changelog = %q", got)
	}
	if !strings.Contains(got, "https://github.com/earendil-works/pi/blob/v1.2.3/packages/coding-agent/docs/a.md") {
		t.Fatalf("links not normalized: %q", got)
	}

	// With the current version there is nothing new.
	settings.SetLastChangelogVersion("1.2.3")
	if got := wiring.GetChangelogForDisplay(); got != "" {
		t.Fatalf("up-to-date changelog = %q", got)
	}
}

// TestStartupMarkdownTheme covers the code-block indent setting.
func TestStartupMarkdownTheme(t *testing.T) {
	wiring, _ := newStartupTestWiring(t)
	base := tui.MarkdownTheme{CodeBlockIndent: "default"}
	// The settings default (two spaces) overrides the base theme.
	if got := wiring.GetMarkdownThemeWithSettings(base); got.CodeBlockIndent != "  " {
		t.Fatalf("indent = %q", got.CodeBlockIndent)
	}
	indent := "    "
	wiring.Settings = coding.NewInMemorySettingsManager(&coding.Settings{
		Markdown: &coding.SettingsMarkdown{CodeBlockIndent: &indent},
	}, coding.SettingsManagerCreateOptions{})
	if got := wiring.GetMarkdownThemeWithSettings(base); got.CodeBlockIndent != "    " {
		t.Fatalf("indent = %q", got.CodeBlockIndent)
	}
}

// TestStartupProviderCount covers the footer provider count.
func TestStartupProviderCount(t *testing.T) {
	wiring, _ := newStartupTestWiring(t)
	provider := coding.NewFooterDataProvider("/tmp/proj", coding.FooterDataProviderOptions{DisableWatch: true})
	defer provider.Dispose()
	wiring.FooterData = provider

	wiring.UpdateAvailableProviderCount()
	if got := provider.GetAvailableProviderCount(); got != 0 {
		t.Fatalf("count = %d", got)
	}

	// Scoped models win over the snapshot.
	session := wiring.Session.(*startupTestSession)
	session.scoped = []coding.ScopedModel{
		{Model: &ai.Model{Provider: "p", ID: "a"}},
		{Model: &ai.Model{Provider: "p", ID: "b"}},
		{Model: &ai.Model{Provider: "q", ID: "c"}},
	}
	wiring.UpdateAvailableProviderCount()
	if got := provider.GetAvailableProviderCount(); got != 2 {
		t.Fatalf("count = %d", got)
	}
}

// TestStartupAnthropicWarning covers the subscription-auth warning.
func TestStartupAnthropicWarning(t *testing.T) {
	wiring, settings := newStartupTestWiring(t)
	warnings := []string{}
	wiring.ShowWarning = func(message string) { warnings = append(warnings, message) }

	// The setting can disable the warning.
	settings.SetWarnings(coding.SettingsWarnings{AnthropicExtraUsage: new(false)})
	wiring.MaybeWarnAboutAnthropicSubscriptionAuth(context.Background(), &ai.Model{Provider: "anthropic"})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	settings.SetWarnings(coding.SettingsWarnings{AnthropicExtraUsage: new(true)})

	// A non-anthropic model is ignored.
	wiring.MaybeWarnAboutAnthropicSubscriptionAuth(context.Background(), &ai.Model{Provider: "openai"})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}

	// An anthropic model without a subscription key is ignored.
	wiring.Session.(*startupTestSession).model = &ai.Model{Provider: "anthropic", ID: "m"}
	wiring.MaybeWarnAboutAnthropicSubscriptionAuth(context.Background(), nil)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
}

// ClearChatAndRenderInitialMessages is the branch-navigation reset: the chat is
// emptied and re-rendered from the session context, so the fork line replaces
// the transcript on screen instead of being appended after the abandoned
// branch. It goes through StartupWiring.RenderInitialMessages, which has to be
// wired in production for that to do anything (it was test-only, so the reset
// silently did nothing and the port's /tree left the old branch on screen).
func TestClearChatAndRenderInitialMessages(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	manager := coding.NewSessionManager(t.TempDir(), &coding.SessionManagerOptions{Persist: new(false)})
	manager.AppendMessage(ai.Message(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "kept entry"}}))

	chat := &tui.Container{}
	transcript := NewTranscriptRenderer(chat, nil, nil, nil, manager)
	chat.AddChild(tui.NewText("stale entry", 0, 0, nil))

	wiring := &StartupWiring{
		Chat:                  chat,
		Transcript:            transcript,
		SessionInfo:           manager,
		RenderInitialMessages: func() { transcript.RenderInitialMessages() },
	}
	wiring.ClearChatAndRenderInitialMessages()

	lines := strings.Join(renderChat(t, chat), "\n")
	if strings.Contains(lines, "stale entry") {
		t.Errorf("the stale transcript survived the reset:\n%s", lines)
	}
	if !strings.Contains(lines, "kept entry") {
		t.Errorf("the session context was not re-rendered:\n%s", lines)
	}
}

// TestStartupRebuildUpdatesFooter covers the updateFooter path on the chat
// rebuild (upstream rebuildChatFromMessages -> renderSessionItems invalidates
// the footer and refreshes the editor border).
func TestStartupRebuildUpdatesFooter(t *testing.T) {
	wiring, _ := newStartupTestWiring(t)
	manager := coding.NewSessionManager("/tmp/proj", &coding.SessionManagerOptions{Persist: new(false)})
	manager.AppendMessage(ai.Message(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}}))
	wiring.SessionInfo = manager
	transcript := NewTranscriptRenderer(wiring.Chat, nil, wiring.Settings, nil, manager)
	wiring.Transcript = transcript
	borderUpdates := 0
	transcript.UpdateEditorBorderColor = func() { borderUpdates++ }

	wiring.RebuildChatFromMessages()
	if borderUpdates != 1 {
		t.Fatalf("editor border updates = %d", borderUpdates)
	}
}

// postRecordUI records owner posts instead of applying them.
type postRecordUI struct {
	tui.TUI
	posts chan func()
}

func (u *postRecordUI) Post(fn func())     { u.posts <- fn }
func (u *postRecordUI) RequestRender(bool) {}

// The startup subscription-auth check runs on its own goroutine (Run starts it
// with `go w.WarnAnthropic(ctx)`), so the warning must reach the transcript
// through the owner post: appending to the chat from that worker mutates the
// tree the loop renders and routes input through (AGENTS.md "UI ownership").
func TestStartupAuthWarningLandsOnTheOwnerLoop(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	capture := &postRecordUI{posts: make(chan func(), 1)}
	app.ui = capture

	app.startup.ShowWarning("subscription auth is active")
	if children := len(app.chat.Children); children != 0 {
		t.Fatalf("the warning mutated the chat on the caller's goroutine: %d children", children)
	}
	select {
	case fn := <-capture.posts:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("the warning never reached the owner loop")
	}
	if len(app.chat.Children) == 0 {
		t.Fatal("the warning did not land on the owner loop")
	}
}

// A single worker still warns: the claim must not suppress the first warning.
func TestStartupAnthropicWarningWarnsOnceForRealAuth(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	InitTheme("dark", false)
	credentials := ai.NewInMemoryCredentialStore()
	if _, err := credentials.Modify(context.Background(), "anthropic", func(*ai.Credential) (*ai.Credential, error) {
		return &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "sk-ant-oat01-test"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	runtime, err := coding.CreateModelRuntime(coding.CreateModelRuntimeOptions{
		Credentials: credentials, DisableModelsJSON: true,
	})
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	settings := coding.NewInMemorySettingsManager(nil, coding.SettingsManagerCreateOptions{})
	var warnings atomic.Int64
	wiring := &StartupWiring{
		Session:  &startupTestSession{runtime: runtime},
		Settings: settings,
		ShowWarning: func(string) {
			warnings.Add(1)
		},
	}
	model := &ai.Model{Provider: "anthropic", ID: "m"}
	wiring.MaybeWarnAboutAnthropicSubscriptionAuth(context.Background(), model)
	wiring.MaybeWarnAboutAnthropicSubscriptionAuth(context.Background(), model)
	if got := warnings.Load(); got != 1 {
		t.Fatalf("warnings = %d, want exactly one", got)
	}
}
