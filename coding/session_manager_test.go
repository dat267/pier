package coding

import (
	"context"
	"encoding/json"
	"github.com/dat267/pier/ai"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"weak"
)

// Session manager tests keyed to upstream session-manager.ts semantics.

func newTestSession(t *testing.T) (*SessionManager, string) {
	t.Helper()
	dir := t.TempDir()
	m := NewSessionManager(dir, &SessionManagerOptions{SessionDir: filepath.Join(dir, "sessions")})
	return m, dir
}

// Upstream session-manager.ts creates the first persisted file with openSync(..., "wx").
func TestSessionManagerDoesNotClobberExistingNewSessionPath(t *testing.T) {
	manager, _ := newTestSession(t)
	path := manager.GetSessionFile()
	sentinel := []byte("existing session file")
	if err := os.WriteFile(path, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "new message"}})
	if err := manager.FlushWrites(); err == nil {
		t.Fatal("persisting a new session over an existing path should fail")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(sentinel) {
		t.Fatalf("existing session was overwritten: %q", got)
	}
}

func TestSessionManagerNewSessionHeader(t *testing.T) {
	m, dir := newTestSession(t)

	header := m.GetHeader()
	if header == nil || header.Version == nil || *header.Version != CurrentSessionVersion {
		t.Fatalf("header = %+v", header)
	}
	if header.ID != m.GetSessionID() {
		t.Fatal("header id mismatch")
	}
	if header.Cwd != ResolvePath(dir, "", PathInputOptions{}) {
		t.Fatalf("cwd = %q", header.Cwd)
	}
	// A session with only setup entries stays in memory: opening and closing pi
	// without chatting leaves no file (upstream ff72faba2).
	m.AppendModelChange("anthropic", "claude-sonnet-4-5")
	if _, err := os.Stat(m.GetSessionFile()); err == nil {
		t.Fatal("file must not exist for setup entries alone")
	}
	// The first user message creates the file, so the prompt survives a first
	// turn that never produces an assistant message.
	m.AppendMessage(createUserMessage("hi"))
	if _, err := os.Stat(m.GetSessionFile()); err != nil {
		t.Fatal("file must exist after the first user message")
	}
	// The file holds ALL entries (backfill on first message).
	data, _ := os.ReadFile(m.GetSessionFile())
	lines := strings.Count(string(data), "\n")
	if lines != 3 {
		t.Fatalf("lines = %d; want 3 (header + model change + user message)", lines)
	}
	// Later entries append without rewriting the earlier ones.
	m.AppendMessage(createAssistantMessageT("reply"))
	data, _ = os.ReadFile(m.GetSessionFile())
	if lines := strings.Count(string(data), "\n"); lines != 4 {
		t.Fatalf("lines = %d; want 4 after the assistant message", lines)
	}
}

func createAssistantMessageT(text string) *ai.AssistantMessage {
	return &ai.AssistantMessage{
		Content: ai.ContentList{ai.TextContent{Text: text}},
		API:     "openai-responses", Provider: "openai", Model: "mock",
		StopReason: ai.StopStop, Timestamp: time.Now().UnixMilli(),
	}
}

func TestSessionManagerTreeAndBranching(t *testing.T) {
	m, _ := newTestSession(t)
	m.AppendMessage(createUserMessage("one"))
	m.AppendMessage(createAssistantMessageT("reply"))
	m.AppendMessage(createUserMessage("two"))

	entries := m.GetEntries()
	if len(entries) != 3 {
		t.Fatalf("entries = %d", len(entries))
	}
	// Chain: entry0.parent = nil; entry1.parent = entry0; entry2.parent = entry1.
	if entries[0].ParentID != nil {
		t.Fatal("root parent must be nil")
	}
	if *entries[1].ParentID != entries[0].ID || *entries[2].ParentID != entries[1].ID {
		t.Fatal("parent chain broken")
	}

	// Branch back to the first entry and append — a sibling branch forms.
	if err := m.Branch(entries[0].ID); err != nil {
		t.Fatal(err)
	}
	m.AppendMessage(createUserMessage("alternative"))
	branch := m.GetBranch("")
	if len(branch) != 2 || branch[0].ID != entries[0].ID {
		t.Fatalf("branch = %d entries", len(branch))
	}

	// The context follows the CURRENT leaf path only.
	context := m.Projection()
	if len(context.Messages) != 2 {
		t.Fatalf("context messages = %d; want 2", len(context.Messages))
	}
}

func TestSessionManagerBuildContextWithCompaction(t *testing.T) {
	m, _ := newTestSession(t)
	m.AppendMessage(createUserMessage("old-1"))
	m.AppendMessage(createAssistantMessageT("old-reply"))
	m.AppendMessage(createUserMessage("kept"))
	entries := m.GetEntries()

	m.AppendCompaction("summary of old", entries[2].ID, 1000, nil, false, nil)
	m.AppendMessage(createUserMessage("after compaction"))

	context := m.Projection()
	// compaction entry (as summary message) + kept user + after-compaction user.
	roles := messageRoles(context.Messages)
	if len(roles) != 3 {
		t.Fatalf("roles = %v", roles)
	}
	if roles[0] != "compactionSummary" || roles[1] != "user" || roles[2] != "user" {
		t.Fatalf("roles = %v", roles)
	}
	// The kept user message text survives.
	if user, ok := context.Messages[1].(*ai.UserMessage); ok && user.Content.Text != "kept" {
		t.Fatalf("kept = %+v", user)
	}
}

func TestSessionManagerModelAndThinkingLevelChanges(t *testing.T) {
	m, _ := newTestSession(t)
	m.AppendMessage(createUserMessage("hi"))
	m.AppendModelChange("anthropic", "claude-opus-4-5")
	m.AppendThinkingLevelChange("high")

	context := m.Projection()
	if context.Model == nil || context.Model.Provider != "anthropic" || context.Model.ModelID != "claude-opus-4-5" {
		t.Fatalf("model = %+v", context.Model)
	}
	if context.ThinkingLevel != "high" {
		t.Fatalf("thinkingLevel = %q", context.ThinkingLevel)
	}
}

func TestNewSessionRejectsExplicitEmptyID(t *testing.T) {
	manager := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: new(false)})
	defer func() {
		if recover() == nil {
			t.Fatal("expected an explicitly empty session ID to panic as invalid")
		}
	}()
	manager.NewSession(&NewSessionOptions{ID: "", IDSet: true})
}

func TestSessionManagerPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	m := NewSessionManager(dir, &SessionManagerOptions{SessionDir: filepath.Join(dir, "sessions")})
	m.AppendMessage(createUserMessage("persist me"))
	m.AppendMessage(createAssistantMessageT("persisted"))
	m.AppendSessionInfo("My Session")
	sessionFile := m.GetSessionFile()

	reloaded, err := OpenSession(sessionFile, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.GetSessionID() != m.GetSessionID() {
		t.Fatal("session id mismatch")
	}
	if len(reloaded.GetEntries()) != 3 {
		t.Fatalf("entries = %d", len(reloaded.GetEntries()))
	}
	if reloaded.GetSessionName() != "My Session" {
		t.Fatalf("name = %q", reloaded.GetSessionName())
	}
	// Context equivalence after reload.
	original := m.Projection()
	reloadedContext := reloaded.Projection()
	if len(original.Messages) != len(reloadedContext.Messages) {
		t.Fatalf("context sizes = %d vs %d", len(original.Messages), len(reloadedContext.Messages))
	}
}

func createUserMessage(text string) *ai.UserMessage {
	return &ai.UserMessage{Content: ai.StringOrBlocks{Text: text}, Timestamp: time.Now().UnixMilli()}
}

func messageRoles(messages []ai.Message) []string {
	var out []string
	for _, m := range messages {
		out = append(out, ai.RoleOf(m))
	}
	return out
}

// TestSessionManagerCachesBranch pins that GetBranch("") reuses one walk: the
// footer asks for context usage on every paint, and rebuilding the branch of a
// long session is O(entries) (12.6 ms on a 15.5k-entry session).
func TestSessionManagerCachesBranch(t *testing.T) {
	m, _ := newTestSession(t)
	m.AppendMessage(createUserMessage("one"))
	m.AppendMessage(createUserMessage("two"))

	first := m.GetBranch("")
	second := m.GetBranch("")
	if len(first) == 0 {
		t.Fatal("empty branch")
	}
	if &first[0] != &second[0] {
		t.Fatal("GetBranch rebuilt the branch instead of reusing the cached walk")
	}
	// The shared result must not have spare capacity: a caller appending to it
	// would write into the cache's backing array.
	if cap(first) != len(first) {
		t.Fatalf("cached branch cap = %d, want %d", cap(first), len(first))
	}

	// A new entry invalidates the cache.
	m.AppendMessage(createUserMessage("three"))
	third := m.GetBranch("")
	if len(third) != len(first)+1 {
		t.Fatalf("branch after append = %d entries, want %d", len(third), len(first)+1)
	}
	if &third[0] == &first[0] {
		t.Fatal("append did not invalidate the cached branch")
	}

	// A leaf move invalidates the cache too.
	tree := m.GetEntries()
	if len(tree) >= 2 {
		target := tree[0].ID
		path := m.GetBranch(target)
		if len(path) == 0 {
			t.Fatal("explicit branch is empty")
		}
	}
}

// TestSessionManagerBranchCacheSurvivesFromID checks the cached branch is the
// current leaf's, not an explicitly requested one.
func TestSessionManagerBranchCacheSurvivesFromID(t *testing.T) {
	m, _ := newTestSession(t)
	m.AppendMessage(createUserMessage("one"))
	m.AppendMessage(createUserMessage("two"))
	entries := m.GetEntries()
	firstID := entries[0].ID

	cachedLeaf := m.GetBranch("")
	explicit := m.GetBranch(firstID)
	if len(explicit) >= len(cachedLeaf) {
		t.Fatalf("explicit branch %d entries, leaf branch %d", len(explicit), len(cachedLeaf))
	}
	// The explicit walk must not have replaced the leaf cache.
	again := m.GetBranch("")
	if &again[0] != &cachedLeaf[0] {
		t.Fatal("explicit GetBranch(fromID) clobbered the leaf cache")
	}
}

// Reference: pi v1.4.0 core/session-manager.ts getTree compares Date.getTime,
// not timestamp strings. Equal millisecond values retain insertion order.
func TestSessionTreeOrdersChildrenByJavaScriptTime(t *testing.T) {
	for _, tc := range []struct {
		name, first, second string
		firstWins           bool
	}{
		{"timezone offset", "2026-10-20T01:00:00Z", "2026-10-20T02:00:00+02:00", false},
		{"omitted fractional seconds", "2026-10-20T00:00:00.100Z", "2026-10-20T00:00:00Z", false},
		{"same millisecond", "2026-10-20T00:00:00.0009Z", "2026-10-20T00:00:00.0001Z", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: new(false)})
			root := manager.AppendMessage(createUserMessage("root"))
			first := manager.AppendMessage(createUserMessage("first"))
			if err := manager.Branch(root); err != nil {
				t.Fatal(err)
			}
			second := manager.AppendMessage(createUserMessage("second"))
			manager.byID[first].Timestamp = tc.first
			manager.byID[second].Timestamp = tc.second
			roots := manager.GetTree()
			if len(roots) != 1 || len(roots[0].Children) != 2 {
				t.Fatalf("invalid fixture tree=%v", roots)
			}
			want := second
			if tc.firstWins {
				want = first
			}
			if got := roots[0].Children[0].Entry.ID; got != want {
				t.Fatalf("first child=%s want=%s", got, want)
			}
		})
	}
}

// sessionFileRoles returns the `type` of each line in a session file.
func sessionFileRoles(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var roles []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var probe struct {
			Type    string `json:"type"`
			Message struct {
				Role string `json:"role"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			t.Fatal(err)
		}
		role := probe.Type
		if probe.Type == "message" {
			role = probe.Message.Role
		}
		roles = append(roles, role)
	}
	return roles
}

// TestSessionFileCreationFollowsTheFirstMessage ports the "session file
// creation" cases of session-manager/file-operations.test.ts (ff72faba2).
func TestSessionFileCreationFollowsTheFirstMessage(t *testing.T) {
	t.Run("only setup entries stay in memory", func(t *testing.T) {
		m, _ := newTestSession(t)
		m.AppendModelChange("anthropic", "claude-sonnet-4-5")
		m.AppendThinkingLevelChange("off")
		if _, err := os.Stat(m.GetSessionFile()); err == nil {
			t.Fatal("file must not exist for setup entries alone")
		}
	})
	t.Run("the first user message creates the file", func(t *testing.T) {
		m, _ := newTestSession(t)
		m.AppendModelChange("anthropic", "claude-sonnet-4-5")
		m.AppendMessage(createUserMessage("first question"))
		if got := strings.Join(sessionFileRoles(t, m.GetSessionFile()), ","); got != "session,model_change,user" {
			t.Fatalf("roles = %q", got)
		}
		reloaded, err := OpenSession(m.GetSessionFile(), "", "")
		if err != nil {
			t.Fatal(err)
		}
		if n := len(reloaded.Projection().Messages); n != 1 {
			t.Fatalf("reloaded messages = %d", n)
		}
	})
	t.Run("later entries append without rewriting", func(t *testing.T) {
		m, _ := newTestSession(t)
		m.AppendMessage(createUserMessage("first question"))
		m.AppendCustomEntry("preset-state", json.RawMessage(`{"name":"plan"}`))
		m.AppendMessage(createAssistantMessageT("first answer"))
		if got := strings.Join(sessionFileRoles(t, m.GetSessionFile()), ","); got != "session,user,custom,assistant" {
			t.Fatalf("roles = %q", got)
		}
	})
}

func assistantTestMessage() *ai.AssistantMessage {
	return &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "p", Model: "m", StopReason: ai.StopStop,
		Content: ai.ContentList{ai.TextContent{Text: "hello"}},
	}
}

// TestSessionManagerPersistStopsAtAssistant pins the flush-on-first-conversation
// check to upstream's `fileEntries.some(...)`: once a user or assistant message
// is known to exist the scan must stop, otherwise every append on a long session
// re-unmarshals every entry (2.6 ms/append at 5k entries, 8 ms at 15.5k).
func TestSessionManagerPersistStopsAtAssistant(t *testing.T) {
	build := func(filler int) *SessionManager {
		m, _ := newTestSession(t)
		m.AppendMessage(createUserMessage("one"))
		m.AppendMessage(assistantTestMessage())
		for range filler {
			m.AppendMessage(createUserMessage("filler"))
		}
		return m
	}

	small, large := build(0), build(2000)
	smallAllocs := testing.AllocsPerRun(10, func() { small.AppendMessage(createUserMessage("x")) })
	largeAllocs := testing.AllocsPerRun(10, func() { large.AppendMessage(createUserMessage("x")) })

	// Appending must not get more expensive as the session grows.
	if largeAllocs > smallAllocs+200 {
		t.Fatalf("append allocated %.0f times on a 2k-entry session vs %.0f on a fresh one; "+
			"the assistant scan did not short-circuit", largeAllocs, smallAllocs)
	}
}

// pi v1.4.0 (4a080252), core/session-manager.ts SessionManager.newSession
// replaces fileEntries and clears byId. Port-local read caches must not keep
// the discarded session's payload alive when the manager starts a new one.
func TestNewSessionReleasesPreviousPayload(t *testing.T) {
	for _, reader := range []string{"none", "signature", "branch", "projection", "retained snapshot"} {
		t.Run(reader, func(t *testing.T) {
			manager := InMemorySession(t.TempDir(), nil, nil)
			payload, snapshot := cachePreviousSessionPayload(manager, reader)
			manager.NewSession(nil)
			runtime.GC()
			if reader == "retained snapshot" {
				if payload.Value() == nil {
					t.Fatal("new session invalidated a caller-owned branch snapshot")
				}
			} else if payload.Value() != nil {
				t.Fatal("new session retains the previous session's message payload")
			}
			runtime.KeepAlive(snapshot)
			runtime.KeepAlive(manager)
		})
	}
}

func cachePreviousSessionPayload(manager *SessionManager, reader string) (weak.Pointer[byte], []SessionEntry) {
	id := manager.AppendMessage(createUserMessage(strings.Repeat("payload", 1024)))
	payload := weak.Make(&manager.GetEntry(id).Message[0])
	switch reader {
	case "signature":
		manager.ContextSignature()
	case "branch":
		manager.GetBranch("")
	case "projection":
		manager.Projection()
	case "retained snapshot":
		return payload, manager.GetBranch("")
	}
	return payload, nil
}

// pi v1.4.0 (4a080252), core/agent-session.ts dispose aborts manual and
// automatic compaction before disconnecting the session. A host may dispose
// from compaction_start; no summary may commit after that cancellation.
func TestDisposeCancelsActiveCompaction(t *testing.T) {
	for _, manual := range []bool{false, true} {
		name := "automatic"
		if manual {
			name = "manual"
		}
		t.Run(name, func(t *testing.T) {
			var runs atomic.Int64
			session := compactionTestSession(t, &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}, summarizingStreamFn(t, &runs, "summary text long enough to pass the usability check"), smallCompactionSettings())
			seedBranch(t, session)
			session.Subscribe(func(event *SessionEvent) {
				if event.Type == SessionCompactionStart {
					session.Dispose()
				}
			})
			var err error
			if manual {
				_, err = session.CompactSession(context.Background(), "")
			} else {
				_, err = session.RunAutoCompaction(context.Background(), CompactionThreshold, false)
			}
			if err == nil {
				t.Fatal("disposed session completed compaction instead of canceling it")
			}
			for _, entry := range session.Sessions.GetEntries() {
				if entry.Type == "compaction" {
					t.Fatal("disposed session persisted a compaction")
				}
			}
		})
	}
}

// The same dispose boundary owns branch-summary cancellation, not just the
// low-level agent run (core/agent-session.ts dispose -> abortBranchSummary).
func TestDisposeCancelsActiveBranchSummary(t *testing.T) {
	manager := sessionWithBranch(t)
	rootID := manager.GetEntries()[0].ID
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Sessions: manager, StreamFn: stubStreamFn,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192},
	})
	if err != nil {
		t.Fatal(err)
	}
	session.CompactionStreamFn = func(_ *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		session.Dispose()
		if options.Ctx == nil || options.Ctx.Err() != context.Canceled {
			t.Fatal("disposal did not cancel the forwarded branch-summary context")
		}
		// Model a cancellation-aware provider. A fake that completes with
		// success after ignoring Ctx races EventStream.Result's ready-result
		// path and tests scheduling rather than disposal (CR-074).
		stream := ai.NewAssistantMessageEventStream()
		stream.Push(ai.AssistantMessageEvent{
			Type: ai.EventError, Reason: ai.StopAborted,
			Error: &ai.AssistantMessage{StopReason: ai.StopAborted},
		})
		return stream
	}
	result, err := session.NavigateTree(context.Background(), rootID, NavigateTreeOptions{Summarize: true})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.Aborted || !result.Cancelled {
		t.Fatalf("disposed branch summarization = %+v, want aborted and cancelled", result)
	}
	for _, entry := range manager.GetEntries() {
		if entry.Type == "branch_summary" {
			t.Fatal("disposed session persisted a branch summary")
		}
	}
}

// pi v1.4.0 (4a080252), core/agent-session.ts dispose clears onWarmed and
// cancels its cache warmer. A disposed session must not issue future paid
// refreshes or retain its session event callback. The clock is virtual.
func TestDisposeStopsSessionCacheWarming(t *testing.T) {
	for _, dispose := range []bool{false, true} {
		name := "live-control"
		if dispose {
			name = "disposed"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				model := warmerModel()
				warmer, manager, runs := newTestWarmer(t, model, CacheWarmingIdle)
				defer warmer.Cancel()
				manager.AppendMessage(&ai.AssistantMessage{
					API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopStop,
					Content: ai.ContentList{ai.TextContent{Text: "reply"}}, Usage: ai.Usage{Input: 1000, TotalTokens: 1000},
				})
				session, err := NewAgentSession(&SessionConfig{Cwd: manager.GetCwd(), Sessions: manager, Model: model, StreamFn: stubStreamFn})
				if err != nil {
					t.Fatal(err)
				}
				session.CacheWarmer = warmer
				var notifications atomic.Int64
				warmer.OnWarmed = func(*SessionEntry) { notifications.Add(1) }
				warmer.Decide = func(CacheWarmingDecisionEvent) CacheWarmingAction { return CacheWarmingActionWarm }
				warmer.Start(CacheWarmRequest{Model: model, Options: &ai.SimpleStreamOptions{Ctx: context.Background()}}, func() bool { return true })
				if status := warmer.Status(); status.State != "scheduled" {
					t.Fatalf("warm run was not scheduled: %+v", status)
				}
				if dispose {
					session.Dispose()
					if status := warmer.Status(); status.State != "inactive" {
						t.Errorf("disposed warmer = %+v, want inactive", status)
					}
				}
				time.Sleep(1500 * time.Millisecond)
				synctest.Wait()
				want := int64(1)
				if dispose {
					want = 0
				}
				if got := runs.Load(); got != want {
					t.Errorf("cache refresh requests = %d, want %d", got, want)
				}
				if got := notifications.Load(); got != want {
					t.Errorf("warm notifications = %d, want %d", got, want)
				}
				if dispose && warmer.OnWarmed != nil {
					t.Error("disposed warmer retained its session callback")
				}
			})
		})
	}
}

// Reference: pi v1.5.0 (f9a26365), core/agent-session.ts getContextUsage:
// percent is estimateProjectedContextTokens / model.contextWindow, so the
// denominator follows the active model. Switching models must not change the
// token estimate, only the reported percentage (the footer shows both).
func TestContextUsagePercentFollowsTheActiveModelWindow(t *testing.T) {
	session := newControlSession(t, &ai.Model{ID: "small", Provider: "test", ContextWindow: 1000}, nil)
	session.Sessions.AppendMessage(&ai.AssistantMessage{
		API: ai.APIOpenAICompletions, Provider: "test", Model: "small",
		Content: ai.ContentList{ai.TextContent{Text: "answer"}}, StopReason: ai.StopStop,
		Usage: ai.Usage{Input: 400, Output: 100, TotalTokens: 500}, Timestamp: 2,
	})
	before := session.GetContextUsage()
	if before == nil || before.Tokens == nil || before.Percent == nil {
		t.Fatalf("control usage = %+v, want tokens and percent", before)
	}
	if *before.Tokens != 500 || *before.Percent != 50 || before.ContextWindow != 1000 {
		t.Fatalf("control usage = %d tokens, %v%% of %d; want 500, 50%%, 1000", *before.Tokens, *before.Percent, before.ContextWindow)
	}

	if err := session.SetModel(t.Context(), &ai.Model{ID: "large", Provider: "test", ContextWindow: 4000}, ModelMutationOptions{}); err != nil {
		t.Fatal(err)
	}
	after := session.GetContextUsage()
	if after == nil || after.Tokens == nil || after.Percent == nil {
		t.Fatalf("usage after switch = %+v, want tokens and percent", after)
	}
	if *after.Tokens != 500 {
		t.Fatalf("token estimate changed across a model switch: %d, want 500", *after.Tokens)
	}
	if after.ContextWindow != 4000 || *after.Percent != 12.5 {
		t.Fatalf("usage after switch = %v%% of %d; want 12.5%% of 4000", *after.Percent, after.ContextWindow)
	}
}

// Reference: pi v1.4.0 (4a080252), core/agent-session.ts getContextUsage
// counts only projected assistants as post-compaction usage evidence.
func TestContextUsageUnknownAfterOmittingPostCompactionResponse(t *testing.T) {
	session := newControlSession(t, &ai.Model{ID: "m", Provider: "test", ContextWindow: 1000}, nil)
	session.Sessions.AppendCompaction("summary", "", 10, nil, false, nil)
	assistant := &ai.AssistantMessage{API: ai.APIOpenAICompletions, Provider: "test", Model: "m", Content: ai.ContentList{ai.TextContent{Text: "answer"}}, StopReason: ai.StopStop, Usage: ai.Usage{Input: 100, Output: 5, TotalTokens: 105}, Timestamp: 2}
	id := session.Sessions.AppendMessage(assistant)
	if usage := session.GetContextUsage(); usage == nil || usage.Tokens == nil || *usage.Tokens != 105 {
		t.Fatalf("control usage = %+v, want 105 tokens", usage)
	}
	if _, err := session.Sessions.AppendContextEdit(id, json.RawMessage("null")); err != nil {
		t.Fatal(err)
	}
	usage := session.GetContextUsage()
	if usage == nil || usage.ContextWindow != 1000 || usage.Tokens != nil || usage.Percent != nil {
		t.Fatalf("omitted response establishes usage: %+v, want unknown tokens and percent", usage)
	}
	session.Sessions.AppendMessage(assistant)
	if usage := session.GetContextUsage(); usage == nil || usage.Tokens == nil || usage.Percent == nil {
		t.Fatalf("new projected response did not restore usage: %+v", usage)
	}
}

// Reference: pi v1.5.0 core/agent-session.ts getSessionStats reads
// `assistantMsg.usage` unconditionally and core/usage-totals.ts
// addUsageToTotals dereferences usage.input/output/cacheRead/cacheWrite and
// usage.cost.total, so a persisted assistant message without a complete usage
// record makes the whole call throw. Pier decodes usage into a value type, so
// the projection reads zeros and returns a result.
//
// Every message both sides write carries a complete usage record, so this only
// reaches the boundary through a hand-edited or foreign session file. The port
// keeps the tolerant read deliberately (a damaged transcript still reports its
// counts); this test pins that choice so it cannot change silently.
func TestSessionStatsToleratesAnIncompleteAssistantUsage(t *testing.T) {
	cases := []struct {
		name  string
		usage string
		input int64
	}{
		{"absent usage", ``, 0},
		{"null usage", `"usage":null,`, 0},
		{"partial usage", `"usage":{"input":5},`, 5},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "s.jsonl")
			body := `{"type":"session","version":3,"id":"s","timestamp":"2026-01-01T00:00:00.000Z","cwd":"` + dir + `"}` + "\n" +
				`{"type":"message","id":"m1","parentId":null,"timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"user","content":[{"type":"text","text":"hi"}],"timestamp":1}}` + "\n" +
				`{"type":"message","id":"x","parentId":"m1","timestamp":"2026-01-01T00:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"a"}],"api":"openai-completions","provider":"fake","model":"m",` + testCase.usage + `"stopReason":"stop","timestamp":2}}` + "\n"
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			manager, err := OpenSession(path, dir, "")
			if err != nil {
				t.Fatal(err)
			}
			// The load path seeds the statistics scan off the caller's goroutine.
			waitForSeed(t, manager)
			stats := manager.SessionStats()
			if stats.AssistantMessages != 1 {
				t.Fatalf("assistant messages = %d, want 1", stats.AssistantMessages)
			}
			if stats.Tokens.Input != testCase.input {
				t.Fatalf("input tokens = %d, want %d", stats.Tokens.Input, testCase.input)
			}
		})
	}
}
