package interactive

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/internal/offloop"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// makePersistedSession creates a persisted session with one full turn so the
// file exists on disk (persistEntry flushes on the first assistant message).
func makePersistedSession(t *testing.T, cwd string, text string) *coding.SessionManager {
	t.Helper()
	return makePersistedSessionIn(t, cwd, "", text)
}

// makePersistedSessionIn is makePersistedSession with an explicit session
// directory ("" = the default store for cwd).
func makePersistedSessionIn(t *testing.T, cwd string, sessionDir string, text string) *coding.SessionManager {
	t.Helper()
	persist := true
	sm := coding.NewSessionManager(cwd, &coding.SessionManagerOptions{SessionDir: sessionDir, Persist: &persist})
	sm.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: text}})
	sm.AppendMessage(&ai.AssistantMessage{
		API:        ai.APIAnthropicMessages,
		Provider:   "anthropic",
		Model:      "m",
		Content:    ai.ContentList{ai.TextContent{Text: "ack"}},
		StopReason: ai.StopStop,
	})
	return sm
}

func TestSwitchSessionSwapsAndRebinds(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())

	targetCwd := t.TempDir()
	target := makePersistedSession(t, targetCwd, "from the other session")

	result, err := app.switchSession(target.GetSessionFile(), "")
	if err != nil {
		t.Fatalf("SwitchSession: %v", err)
	}
	if result == nil || result.Cancelled {
		t.Fatalf("switch cancelled: %+v", result)
	}
	if app.sessionMgr.GetSessionID() != target.GetSessionID() {
		t.Fatalf("app session manager id %s, want %s", app.sessionMgr.GetSessionID(), target.GetSessionID())
	}
	// The AppSession adapter is shared by every wiring; the embedded agent
	// session must follow the swap.
	if app.session.SessionID() != target.GetSessionID() {
		t.Fatalf("app session id %s, want %s", app.session.SessionID(), target.GetSessionID())
	}
	// The session-info holders must all point at the new manager.
	for _, info := range []*coding.SessionManager{
		app.sessions.SessionInfo, app.commands.SessionInfo, app.events.SessionInfo,
		app.startup.SessionInfo, app.selectors.SessionInfo, app.trust.SessionInfo,
		app.autocomplete.SessionInfo, app.transcript.SessionInfo,
	} {
		if info.GetSessionID() != target.GetSessionID() {
			t.Fatalf("wiring still holds session %s, want %s", info.GetSessionID(), target.GetSessionID())
		}
	}
	// The transcript was rebuilt from the new session.
	if len(app.chat.Children) == 0 {
		t.Fatal("chat is empty after switching to a session with messages")
	}
	// The event pump is re-attached to the new session.
	if app.unsubscribe == nil {
		t.Fatal("session events are not subscribed after the switch")
	}
}

func TestSwitchSessionMissingCwdIsPromptable(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())

	targetCwd := t.TempDir()
	target := makePersistedSession(t, targetCwd, "gone")
	if err := os.Remove(targetCwd); err != nil {
		t.Fatalf("remove cwd: %v", err)
	}

	_, err := app.switchSession(target.GetSessionFile(), "")
	if err == nil {
		t.Fatal("expected an error for a missing session cwd")
	}
	// The retry path keys on the typed error (upstream MissingSessionCwdError),
	// and it has to carry the issue the prompt offers: the missing cwd and the
	// fallback to continue in.
	var cwdErr *coding.MissingSessionCwdError
	if !errors.As(err, &cwdErr) {
		t.Fatalf("error %q is not a MissingSessionCwdError", err)
	}
	if cwdErr.Issue.SessionCwd != targetCwd {
		t.Errorf("issue cwd = %q, want %q", cwdErr.Issue.SessionCwd, targetCwd)
	}
	if cwdErr.Issue.FallbackCwd == "" {
		t.Error("the issue carries no fallback cwd for the prompt")
	}
	if err.Error() != coding.FormatMissingSessionCwdError(cwdErr.Issue) {
		t.Errorf("error %q does not match the upstream format", err)
	}
}

func TestSessionNewStartsFreshSession(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	previous := app.sessionMgr.GetSessionID()
	result, err := app.sessionNew()
	if err != nil {
		t.Fatalf("SessionNew: %v", err)
	}
	if result == nil || result.Cancelled {
		t.Fatalf("new session cancelled: %+v", result)
	}
	if app.sessionMgr.GetSessionID() == previous {
		t.Fatal("session id did not change")
	}
	if app.session.SessionID() != app.sessionMgr.GetSessionID() {
		t.Fatalf("app session id %s does not match the manager %s",
			app.session.AgentSession.SessionID(), app.sessionMgr.GetSessionID())
	}
	if len(app.chat.Children) != 0 {
		t.Fatalf("chat should be empty for a fresh session, got %d children",
			len(app.chat.Children))
	}
}

func TestSwitchSessionUpdatesFooterCwd(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())

	targetCwd := t.TempDir()
	target := makePersistedSession(t, targetCwd, "other project")

	if _, err := app.switchSession(target.GetSessionFile(), ""); err != nil {
		t.Fatalf("SwitchSession: %v", err)
	}
	if app.footerData.Cwd() != targetCwd {
		t.Fatalf("footer cwd %s, want %s", app.footerData.Cwd(), targetCwd)
	}
}

// A session rebind replaces the AgentSession (and with it the goal controller),
// so the idle-round launcher must be rewired or /goal resume would stop
// starting a run after /new, /resume or /fork.
func TestSwitchSessionRewiresGoalStartWork(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	if controller := app.session.GoalController(); controller != nil && !controller.StartWorkWired() {
		t.Fatal("goal launcher not wired at construction")
	}
	target := makePersistedSession(t, t.TempDir(), "from the other session")
	if _, err := app.switchSession(target.GetSessionFile(), ""); err != nil {
		t.Fatalf("switchSession: %v", err)
	}
	controller := app.session.GoalController()
	if controller == nil {
		t.Skip("goals are disabled for this session")
	}
	if !controller.StartWorkWired() {
		t.Fatal("goal launcher not rewired after a session switch")
	}
}

// Reference: pi v1.5.0 (f9a26365), interactive-mode.ts
// rebindCurrentSession/subscribeToAgent keep UI events on the same JS owner.
// D143 requires the Go replacement subscription to enqueue like initial Init.
func TestSessionReplacementStillQueuesEventsForTheUIOwner(t *testing.T) {
	for _, mode := range []string{"initial session", "new session", "switched session"} {
		t.Run(mode, func(t *testing.T) {
			app, cleanup := newTestApp(t)
			defer cleanup()
			app.Init(context.Background())
			switch mode {
			case "new session":
				if _, err := app.sessionNew(); err != nil {
					t.Fatal(err)
				}
			case "switched session":
				target := makePersistedSession(t, t.TempDir(), "incoming session")
				if _, err := app.switchSession(target.GetSessionFile(), ""); err != nil {
					t.Fatal(err)
				}
			}

			var titleUpdates atomic.Int64
			app.events.UpdateTerminalTitle = func() { titleUpdates.Add(1) }
			produced := make(chan struct{})
			go func() {
				defer close(produced)
				app.session.SetSessionName("named by a worker")
			}()
			<-produced
			if got := titleUpdates.Load(); got != 0 {
				t.Fatalf("worker applied %d title updates before the UI owner drained events", got)
			}
			select {
			case event := <-app.sessionEvents.Events():
				if event.Type != coding.SessionInfoChanged {
					t.Fatalf("queued event = %s, want session_info_changed", event.Type)
				}
				app.events.handleQueuedEvent(event)
			default:
				t.Fatal("session event was not queued for the UI owner")
			}
			if got := titleUpdates.Load(); got != 1 {
				t.Fatalf("owner applied %d title updates, want 1", got)
			}
		})
	}
}

// A485: a session switch must keep the write queue. OpenSession builds a
// manager with no queue, so without forwarding it the new session writes its
// file synchronously on the calling goroutine — for the interactive path the
// owner loop. cmd/main.go:343-351 wires the queue for exactly this reason and
// sessionNew forwards it explicitly; switchSession did not.
//
// Invariant: ARCHITECTURE.md, "nothing on the main event loop may block", and
// SessionManagerOptions.WriteQueue, which documents nil as "one-shot
// consumers" only.
func TestSwitchSessionKeepsTheWriteQueue(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())

	group := offloop.NewGroup()
	defer group.StopAll()
	queue := group.Queue()
	app.sessionMgr.SetWriteQueue(queue)
	if app.sessionMgr.GetWriteQueue() != queue {
		t.Fatal("fixture did not install the queue on the starting manager")
	}

	target := makePersistedSession(t, t.TempDir(), "other session")
	if _, err := app.switchSession(target.GetSessionFile(), ""); err != nil {
		t.Fatalf("switchSession: %v", err)
	}
	if got := app.sessionMgr.GetWriteQueue(); got != queue {
		t.Fatalf("after switching, the session manager's write queue is %v, want the previous queue %v (nil means synchronous writes on the owner loop)", got, queue)
	}
}

// A485 covers every replacement path, not only switchSession: sessionNew and
// import also build a manager through OpenSession/NewSessionManager.
func TestSessionNewKeepsTheWriteQueue(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())

	group := offloop.NewGroup()
	defer group.StopAll()
	queue := group.Queue()
	app.sessionMgr.SetWriteQueue(queue)

	if _, err := app.sessionNew(); err != nil {
		t.Fatalf("sessionNew: %v", err)
	}
	if got := app.sessionMgr.GetWriteQueue(); got != queue {
		t.Fatalf("after sessionNew the write queue is %v, want %v", got, queue)
	}
}

// Reference: pi v1.5.0 (f9a26365), interactive-mode.ts
// rebindCurrentSession replaces the outgoing event binding. D143's Go handoff
// must reject a listener snapshot that resumes after that binding is retired.
func TestSessionReplacementRejectsAnAlreadyAdmittedOldListener(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()

	// This subscriber precedes the app's subscription. AgentSession.emit has
	// already snapshotted both callbacks when the first one pauses delivery.
	app.session.Subscribe(func(event *coding.SessionEvent) {
		if event.Type == coding.SessionMessageStart {
			close(entered)
			<-release
		}
	})
	app.Init(context.Background())
	outgoing := app.session.AgentSession
	produced := make(chan struct{})
	go func() {
		defer close(produced)
		outgoing.AppendCustomMessage(&ai.CustomMessage{
			Role:    coding.RoleCustom,
			Content: json.RawMessage(`{"customType":"note","content":"late outgoing note","display":true}`),
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("old event did not reach its admitted listener")
	}
	if _, err := app.sessionNew(); err != nil {
		t.Fatal(err)
	}
	unblock()
	select {
	case <-produced:
	case <-time.After(5 * time.Second):
		t.Fatal("old event producer did not finish")
	}
	app.runner.drainReadyEvents()
	chat := coding.StripAnsi(strings.Join(app.chat.Render(120), "\n"))
	if strings.Contains(chat, "late outgoing note") {
		t.Fatalf("an admitted outgoing listener contaminated the replacement transcript:\n%s", chat)
	}
}
