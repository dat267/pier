package coding

import (
	"context"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// TestSessionFileRoundTripPreservesInvariants pins the resume contract end to
// end over a real session file: create a session, run a turn, set a goal, then
// reopen the file and build a second session from it. The active loadout, the
// model, the thinking level, the messages and the durable goal must all survive
// the round trip. This is the class of bug that lost every built-in tool on the
// first resume (D237): a persistence field that the load path never read.
func TestSessionFileRoundTripPreservesInvariants(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	if err := runtime.SetRuntimeAPIKey("anthropic", "sk-1", context.Background()); err != nil {
		t.Fatal(err)
	}
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	settings.SetDefaultProvider("anthropic")
	cwd := t.TempDir()
	manager := NewSessionManager(cwd, nil)

	created, err := CreateAgentSession(context.Background(), &CreateAgentSessionOptions{
		Cwd:             cwd,
		ModelRuntime:    runtime,
		SettingsManager: settings,
		SessionManager:  manager,
		StreamFn:        stubStreamFn,
		ThinkingLevel:   ai.ThinkHigh,
	})
	if err != nil {
		t.Fatal(err)
	}
	session := created.Session
	if !session.HasModel() {
		t.Fatalf("created session has no model: %q", created.ModelFallbackMessage)
	}
	if err := session.PromptText(context.Background(), "remember 41"); err != nil {
		t.Fatal(err)
	}
	// Set the goal after the turn, with the scheduler paused, so it appends its
	// durable state without starting a round the stub stream would have to
	// serve (an active goal keeps asking for another round).
	session.Goal.SetStartWork(func(func(context.Context) error) {})
	session.Goal.HandleCommand("set round-trip objective")
	goal := session.Goal.Snapshot().Goal
	if goal == nil {
		t.Fatal("goal not set")
	}

	wantTools := strings.Join(session.GetActiveToolNames(), ",")
	wantProvider, wantModelID := session.Model().Provider, session.Model().ID
	wantThinking := session.ThinkingLevel()
	wantMessages := len(session.Messages())
	wantGoal := goal.Objective

	path := manager.sessionFile
	if path == "" {
		t.Fatal("session manager has no file")
	}
	loaded, err := OpenSession(path, manager.SessionDir(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := CreateAgentSession(context.Background(), &CreateAgentSessionOptions{
		Cwd:             cwd,
		ModelRuntime:    runtime,
		SettingsManager: settings,
		SessionManager:  loaded,
		StreamFn:        stubStreamFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := resumed.Session

	if got := strings.Join(r.GetActiveToolNames(), ","); got != wantTools {
		t.Fatalf("resumed tools = %q; want %q", got, wantTools)
	}
	if !r.HasModel() || r.Model().Provider != wantProvider || r.Model().ID != wantModelID {
		t.Fatalf("resumed model = %+v; want %s/%s", r.Model(), wantProvider, wantModelID)
	}
	if got := r.ThinkingLevel(); got != wantThinking {
		t.Fatalf("resumed thinking = %q; want %q", got, wantThinking)
	}
	if got := len(r.Messages()); got != wantMessages {
		t.Fatalf("resumed messages = %d; want %d", got, wantMessages)
	}
	if !userMessagesContain(r.Messages(), "remember 41") {
		t.Fatal("resumed transcript lost the user prompt")
	}
	if r.Goal == nil {
		t.Fatal("resumed session has no goal controller")
	}
	if resumedGoal := r.Goal.Snapshot().Goal; resumedGoal == nil || resumedGoal.Objective != wantGoal {
		t.Fatalf("resumed goal = %+v; want objective %q", resumedGoal, wantGoal)
	}
}

// userMessagesContain reports whether any user message carries the given text.
func userMessagesContain(messages []ai.Message, want string) bool {
	for _, message := range messages {
		if user, ok := message.(*ai.UserMessage); ok && strings.Contains(ai.ContentText(user.Content, ""), want) {
			return true
		}
	}
	return false
}
