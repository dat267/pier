package interactive

import (
	"context"
	"errors"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInteractiveModeHelpers covers the helper layer of interactive-mode.ts.
func TestInteractiveModeHelpers(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	if got := QuoteIfNeeded("simple-1.2_x"); got != "simple-1.2_x" {
		t.Fatalf("quote simple = %q", got)
	}
	if got := QuoteIfNeeded("/tmp/a b"); got != "'/tmp/a b'" {
		t.Fatalf("quote space = %q", got)
	}
	if got := QuoteIfNeeded("it's"); got != `'it'\''s'` {
		t.Fatalf("quote apostrophe = %q", got)
	}
	if got := QuoteIfNeeded(""); got != "''" {
		t.Fatalf("quote empty = %q", got)
	}

	if !IsAnthropicSubscriptionAuthKey("sk-ant-oat01-xyz") {
		t.Fatal("subscription key not detected")
	}
	if IsAnthropicSubscriptionAuthKey("sk-ant-api03") || IsAnthropicSubscriptionAuthKey("") {
		t.Fatal("api key misdetected")
	}
	if !IsUnknownModel(&ai.Model{Provider: "unknown", ID: "unknown", API: "unknown"}) {
		t.Fatal("unknown model not detected")
	}
	if IsUnknownModel(&ai.Model{Provider: "unknown", ID: "unknown"}) || IsUnknownModel(nil) {
		t.Fatal("unknown model misdetected")
	}
	if !HasDefaultModelProvider("anthropic") {
		t.Fatal("anthropic should have a default model")
	}
	if HasDefaultModelProvider("nope") {
		t.Fatal("unexpected default model provider")
	}
	if got := LlamaCppPostLoginGuidance("Logged in", 0); got != "Logged in. No llama.cpp models are loaded. Use /llama to load a model, then /model to select it." {
		t.Fatalf("guidance empty = %q", got)
	}
	if got := LlamaCppPostLoginGuidance("Logged in", 3); got != "Logged in. Use /model to select a loaded llama.cpp model, or /llama to manage models." {
		t.Fatalf("guidance loaded = %q", got)
	}

	options := GetLoginProviderCompletionOptions([]AuthSelectorProvider{
		{ID: "openai", Name: "OpenAI", AuthType: "oauth"},
		{ID: "openai", Name: "OpenAI", AuthType: "api_key"},
		{ID: "anthropic", Name: "Anthropic", AuthType: "api_key"},
		{ID: "openai", Name: "OpenAI", AuthType: "oauth"},
	})
	if len(options) != 2 {
		t.Fatalf("options = %+v", options)
	}
	// Sorted by name; auth types ordered oauth before api_key.
	if options[0].ID != "anthropic" || options[1].ID != "openai" {
		t.Fatalf("order = %+v", options)
	}
	if len(options[1].AuthTypes) != 2 || options[1].AuthTypes[0] != "oauth" || options[1].AuthTypes[1] != "api_key" {
		t.Fatalf("auth types = %+v", options[1].AuthTypes)
	}
	if got := GetLoginProviderSearchText(options[1]); got != "openai OpenAI oauth subscription api_key API key" {
		t.Fatalf("search text = %q", got)
	}
	if got := FormatLoginProviderCompletionDescription(options[1]); got != "OpenAI · subscription/API key" {
		t.Fatalf("description = %q", got)
	}
	same := LoginProviderCompletionOption{ID: "openai", Name: "openai", AuthTypes: []string{"oauth"}}
	if got := FormatLoginProviderCompletionDescription(same); got != "subscription" {
		t.Fatalf("same-name description = %q", got)
	}

	// pi v1.5.2 getLoginProviderCompletionOptions sorts with compareProviders,
	// so a preferred partner stays ahead of a non-preferred provider whose name
	// sorts first.
	preferred := GetLoginProviderCompletionOptions([]AuthSelectorProvider{
		{ID: "alpha", Name: "Alpha", AuthType: "api_key"},
		{ID: "openrouter", Name: "OpenRouter", AuthType: "api_key"},
	})
	if len(preferred) != 2 || preferred[0].ID != "openrouter" {
		t.Fatalf("preferred order = %+v", preferred)
	}
	// The option carries the provider's subscription flag, which decides the
	// oauth label ("account" when false).
	notSubscription := false
	account := GetLoginProviderCompletionOptions([]AuthSelectorProvider{
		{ID: "radius", Name: "Radius", AuthType: "oauth", Subscription: &notSubscription},
	})
	if got := FormatLoginProviderCompletionDescription(account[0]); got != "Radius · account" {
		t.Fatalf("account description = %q", got)
	}
	if got := GetLoginProviderSearchText(account[0]); got != "radius Radius oauth account" {
		t.Fatalf("account search text = %q", got)
	}

	items := []string{"alpha", "beta", "gamma"}
	mapped := CreateFuzzyAutocompleteItems(items, "ga", func(item string) string { return item },
		func(item string) tui.AutocompleteItem { return tui.AutocompleteItem{Value: item} })
	if len(mapped) != 1 || mapped[0].Value != "gamma" {
		t.Fatalf("autocomplete items = %+v", mapped)
	}
	if got := CreateFuzzyAutocompleteItems(items, "zz", func(item string) string { return item },
		func(item string) tui.AutocompleteItem { return tui.AutocompleteItem{Value: item} }); got != nil {
		t.Fatalf("no-match items = %+v", got)
	}
}

// TestExpandableTextAndGuards covers the small components.
func TestExpandableTextAndGuards(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	text := NewExpandableText(func() string { return "collapsed" }, func() string { return "expanded" }, false, 0, 0)
	firstLine := func() string {
		lines := text.Render(20)
		if len(lines) == 0 {
			return ""
		}
		return strings.TrimSpace(lines[0])
	}
	if got := firstLine(); got != "collapsed" {
		t.Fatalf("collapsed render = %q", got)
	}
	text.SetExpanded(true)
	if got := firstLine(); got != "expanded" {
		t.Fatalf("expanded render = %q", got)
	}
	text.SetExpanded(false)
	if got := firstLine(); got != "collapsed" {
		t.Fatalf("re-collapsed render = %q", got)
	}
	if _, ok := IsExpandable(text); !ok {
		t.Fatal("expandable not detected")
	}
	if _, ok := IsExpandable("nope"); ok {
		t.Fatal("unexpected expandable")
	}

	editor := NewCustomEditor(editorTestHost{}, tui.EditorTheme{}, NewAppKeybindingsManager(nil, ""), CustomEditorOptions{
		EmbedWorkingStatus: true,
	})
	statusEditor, ok := IsWorkingStatusEditor(editor)
	if !ok {
		t.Fatal("working status editor not detected")
	}
	if statusEditor != WorkingStatusEditor(editor) {
		t.Fatal("wrong editor returned")
	}
	plain := NewCustomEditor(editorTestHost{}, tui.EditorTheme{}, NewAppKeybindingsManager(nil, ""), CustomEditorOptions{})
	if _, ok := IsWorkingStatusEditor(plain); ok {
		t.Fatal("unexpected working status editor")
	}
}

// TestFormatResumeCommand covers the resume command assembly.
func TestFormatResumeCommand(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "sessions")
	manager := coding.NewSessionManager("/tmp/proj", &coding.SessionManagerOptions{SessionDir: sessionDir})
	if !manager.IsPersisted() {
		t.Fatal("session should be persisted")
	}
	if command := FormatResumeCommand(manager, "", false); command != "" {
		t.Fatalf("non-tty command = %q", command)
	}
	// The session file is written lazily on the first append; create it so the
	// existsSync branch is exercised.
	if err := os.WriteFile(manager.GetSessionFile(), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}
	command := FormatResumeCommand(manager, "pier", true)
	if command == "" {
		t.Fatal("missing command")
	}
	want := "pier" + " --session-dir " + QuoteIfNeeded(manager.GetSessionDir()) + " --session " + manager.GetSessionID()
	if command != want {
		t.Fatalf("command = %q, want %q", command, want)
	}

	// Non-persisted managers produce no command.
	notPersisted := coding.NewSessionManager("/tmp/proj", &coding.SessionManagerOptions{Persist: new(false)})
	if command := FormatResumeCommand(notPersisted, "", true); command != "" {
		t.Fatalf("non-persisted command = %q", command)
	}
}

// queueingSession models the real session's queue: steering and follow-up
// messages land in the same lists ClearQueue drains.
type queueingSession struct{ *queueTestSession }

func (s queueingSession) Steer(message ai.Message) {
	s.queueTestSession.Steer(message)
	if user, ok := message.(*ai.UserMessage); ok {
		s.steering = append(s.steering, user.Content.Text)
	}
}

func (s queueingSession) FollowUp(message ai.Message) {
	s.queueTestSession.FollowUp(message)
	if user, ok := message.(*ai.UserMessage); ok {
		s.followUp = append(s.followUp, user.Content.Text)
	}
}

// pi v1.4.0 (4a080252) interactive-mode.ts flushCompactionQueue: followUp()
// pushes into the session queue synchronously, the prompt failure's catch
// clears the session queue, and only then does the pending rest list resume.
// The restored messages therefore live in the compaction queue exactly once.
// The port restores first and queues afterwards, so the rest message survives
// in both places and would be shown and sent twice.
func TestCompactionQueueFailureDoesNotDuplicateQueuedMessages(t *testing.T) {
	controller, session, _ := newQueueTestController(t)
	queueing := queueingSession{session}
	controller.Session = queueing
	controller.ShowError = func(string) {}

	controller.QueueCompactionMessage("first", "followUp")
	controller.QueueCompactionMessage("second", "followUp")
	session.promptErr = errors.New("boom")
	controller.FlushCompactionQueue(context.Background(), false)

	_, followUp := controller.GetAllQueuedMessages()
	occurrences := 0
	for _, message := range followUp {
		if message == "second" {
			occurrences++
		}
	}
	if occurrences != 1 {
		t.Fatalf("restored follow-up messages = %v, want the failed turn's rest message once", followUp)
	}
}
