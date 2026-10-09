package durable

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of harness/context.ts.

func userMessage(text string, timestamp int64) *ai.UserMessage {
	return &ai.UserMessage{Content: ai.StringOrBlocks{Text: text}, Timestamp: timestamp}
}

func assistantWithCall(id, name string, timestamp int64) *ai.AssistantMessage {
	return &ai.AssistantMessage{
		Content: ai.ContentList{ai.ToolCall{ID: id, Name: name}}, StopReason: ai.StopToolUse, Timestamp: timestamp,
	}
}

func toolResult(id, name, text string, timestamp int64) *ai.ToolResultMessage {
	return &ai.ToolResultMessage{
		ToolCallID: id, ToolName: name, Content: ai.UserContentList{ai.TextContent{Text: text}}, Timestamp: timestamp,
	}
}

func contextEntry(id, conversationID Id, head *Id, model []ai.Message, edits []ContextEdit) StorageWrite {
	return StorageWrite{Type: "entry", Entry: &EntryRecord{
		ID: id, ConversationID: conversationID, Kind: "message", Model: model, Head: head, Edits: edits,
	}}
}

func TestDeriveContextLeadsWithInitialSystemMessage(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()
	root := RootConversationID
	mustCommit(t, storage, conversationWrite(root))
	first := userMessage("first", 1)
	steered := userMessage("steered", 2)
	initialSystem := &ai.SystemMessage{Content: ai.StringOrBlocks{Text: "preamble"}, Timestamp: 3}
	assistant := &ai.AssistantMessage{StopReason: ai.StopStop, Timestamp: 4}
	next := userMessage("next", 5)
	laterSystem := &ai.SystemMessage{Content: ai.StringOrBlocks{Text: "cwd"}, Timestamp: 6}
	mustCommit(t, storage,
		contextEntry(10, root, nil, []ai.Message{first}, nil),
		contextEntry(11, root, nil, []ai.Message{steered}, nil),
		contextEntry(12, root, nil, []ai.Message{initialSystem}, nil),
		contextEntry(13, root, nil, []ai.Message{assistant}, nil),
		contextEntry(14, root, nil, []ai.Message{next}, nil),
		contextEntry(15, root, nil, []ai.Message{laterSystem}, nil),
	)
	bounds, err := CaptureContextBounds(ctx, storage, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := DeriveContext(ctx, storage, root, bounds)
	if err != nil {
		t.Fatal(err)
	}
	describe := func(message ai.Message) string {
		switch typed := message.(type) {
		case *ai.SystemMessage:
			return "system:" + typed.Content.Text
		case *ai.UserMessage:
			return "user:" + typed.Content.Text
		case *ai.AssistantMessage:
			return "assistant"
		default:
			return "unexpected message"
		}
	}
	want := []string{"system:preamble", "user:first", "user:steered", "assistant", "user:next", "system:cwd"}
	if len(view.Messages) != len(want) {
		t.Fatalf("messages = %+v", view.Messages)
	}
	for index, message := range view.Messages {
		if got := describe(message); got != want[index] {
			t.Fatalf("messages[%d] = %q, want %q", index, got, want[index])
		}
	}
	if describe(view.Contributions[0][0]) != "user:first" ||
		describe(view.Contributions[2][0]) != "system:preamble" ||
		describe(view.Contributions[5][0]) != "system:cwd" {
		t.Fatalf("contributions changed order: %+v", view.Contributions)
	}
}

type contextScanCall struct {
	query EntryQuery
	limit int
}

type contextScanStorage struct {
	Storage
	calls []contextScanCall
}

func (s *contextScanStorage) ScanEntries(ctx context.Context, query EntryQuery, cursor Cursor, limit int) (Page[EntryRecord], error) {
	s.calls = append(s.calls, contextScanCall{query: query, limit: limit})
	return s.Storage.ScanEntries(ctx, query, cursor, limit)
}

// Port of cached view isolation from packages/durable/src/harness/context.ts at pi v1.1.0 commit da866ada1.
func TestCachedContextViewsCannotMutateKeptRange(t *testing.T) {
	storage := &contextScanStorage{Storage: NewMemoryStorage()}
	session := NewSession(storage)
	ctx := context.Background()
	root := RootConversationID
	mustCommit(t, storage,
		conversationWrite(root),
		contextEntry(10, root, nil, []ai.Message{userMessage("first", 1)}, nil),
	)
	scheduler := NewTaskScheduler(TaskSchedulerOptions{Session: session, Storage: storage})
	defer scheduler.Seal()
	invocation := NewInvocation(100, root, "run", ctx)
	defer invocation.End()
	runtime := TaskRuntime(&schedulerRuntime{scheduler: scheduler, invocation: invocation, phase: &runtimePhase{}})
	first, err := runtime.Context(root, ctx)
	if err != nil {
		t.Fatal(err)
	}
	first.Messages[0].(*ai.UserMessage).Content.Text = "changed messages"
	first.Contributions[0][0].(*ai.UserMessage).Content.Text = "changed contribution"
	first.Entries[0].Model[0].(*ai.UserMessage).Content.Text = "changed entry"

	second, err := runtime.Context(root, ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []ai.Message{second.Messages[0], second.Contributions[0][0], second.Entries[0].Model[0]} {
		if text := message.(*ai.UserMessage).Content.Text; text != "first" {
			t.Fatalf("cached context was mutated: %q", text)
		}
	}
}

// Port of packages/durable/src/harness/context.ts at pi v1.1.0 commit 68ccef176.
func TestInvocationContextScansOnlyEntriesAfterCachedTail(t *testing.T) {
	base := NewMemoryStorage()
	storage := &contextScanStorage{Storage: base}
	session := NewSession(storage)
	ctx := context.Background()
	root := RootConversationID
	mustCommit(t, storage,
		conversationWrite(root),
		contextEntry(10, root, nil, []ai.Message{userMessage("first", 1)}, nil),
	)
	scheduler := NewTaskScheduler(TaskSchedulerOptions{Session: session, Storage: storage})
	defer scheduler.Seal()
	invocation := NewInvocation(100, root, "run", ctx)
	defer invocation.End()
	runtime := TaskRuntime(&schedulerRuntime{
		scheduler: scheduler, invocation: invocation, phase: &runtimePhase{},
	})
	first, err := runtime.Context(root, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Messages) != 1 || first.Messages[0].(*ai.UserMessage).Content.Text != "first" {
		t.Fatalf("first context = %+v", first.Messages)
	}
	previousTail := Id(10)
	mustCommit(t, storage, contextEntry(12, root, nil, []ai.Message{userMessage("second", 2)}, nil))

	callStart := len(storage.calls)
	second, err := runtime.Context(root, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Messages) != 2 || second.Messages[1].(*ai.UserMessage).Content.Text != "second" {
		t.Fatalf("second context = %+v", second.Messages)
	}
	if got := len(storage.calls) - callStart; got != 2 {
		t.Fatalf("storage scans on second read = %d, want tail lookup and incremental scan", got)
	}
	incremental := storage.calls[callStart+1]
	if incremental.query.MinEntryID == nil || *incremental.query.MinEntryID != previousTail+1 ||
		incremental.query.MaxEntryID == nil || *incremental.query.MaxEntryID != 12 || incremental.limit != contextScanPageSize {
		t.Fatalf("incremental scan = %+v, want ids 11 through 12", incremental)
	}

	mustCommit(t, storage, contextEntry(14, root, nil, nil, []ContextEdit{{Target: 10, Action: EditOmit}}))
	callStart = len(storage.calls)
	edited, err := runtime.Context(root, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(edited.Messages) != 1 || edited.Messages[0].(*ai.UserMessage).Content.Text != "second" {
		t.Fatalf("edited context = %+v", edited.Messages)
	}
	if len(storage.calls)-callStart != 2 {
		t.Fatalf("edit extension scans = %+v", storage.calls[callStart:])
	}
	incremental = storage.calls[callStart+1]
	if incremental.query.MinEntryID == nil || *incremental.query.MinEntryID != 13 {
		t.Fatalf("edit extension scan = %+v", incremental)
	}

	// TaskRuntime.context takes cutoff through options.at in pi v1.1.0 commit 636703a0a.
	previousTail = 12
	callStart = len(storage.calls)
	cutoff, err := runtime.Context(root, ctx, ConversationContextOptions{At: &previousTail})
	if err != nil {
		t.Fatal(err)
	}
	if len(cutoff.Messages) != 2 || cutoff.Messages[0].(*ai.UserMessage).Content.Text != "first" ||
		cutoff.Messages[1].(*ai.UserMessage).Content.Text != "second" || len(storage.calls)-callStart != 1 {
		t.Fatalf("earlier cutoff = %+v, scans = %+v", cutoff.Messages, storage.calls[callStart:])
	}

	head := Id(16)
	mustCommit(t, storage, contextEntry(head, root, &head, []ai.Message{userMessage("fresh", 3)}, nil))
	callStart = len(storage.calls)
	reset, err := runtime.Context(root, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(reset.Messages) != 1 || reset.Messages[0].(*ai.UserMessage).Content.Text != "fresh" ||
		len(storage.calls)-callStart != 2 || storage.calls[callStart+1].query.MinEntryID == nil ||
		*storage.calls[callStart+1].query.MinEntryID != head {
		t.Fatalf("new head did not replace cached range: messages=%+v scans=%+v", reset.Messages, storage.calls[callStart:])
	}
}

// Port of incremental tool-result ordering from packages/durable/src/harness/context.ts at pi v1.1.0 commit da866ada1.
func TestContextExtensionsReorderNewToolResultsAndPreserveEarlierPairs(t *testing.T) {
	storage := &contextScanStorage{Storage: NewMemoryStorage()}
	session := NewSession(storage)
	ctx := context.Background()
	root := RootConversationID
	mustCommit(t, storage,
		conversationWrite(root),
		contextEntry(10, root, nil, []ai.Message{assistantWithCall("c1", "echo", 1)}, nil),
	)
	scheduler := NewTaskScheduler(TaskSchedulerOptions{Session: session, Storage: storage})
	defer scheduler.Seal()
	invocation := NewInvocation(100, root, "run", ctx)
	defer invocation.End()
	runtime := TaskRuntime(&schedulerRuntime{scheduler: scheduler, invocation: invocation, phase: &runtimePhase{}})
	read := func() ContextView {
		t.Helper()
		view, err := runtime.Context(root, ctx)
		if err != nil {
			t.Fatal(err)
		}
		return view
	}
	first := read()
	if len(first.Messages) != 2 || first.Messages[1].(*ai.ToolResultMessage).ToolCallID != "c1" ||
		!first.Messages[1].(*ai.ToolResultMessage).IsError {
		t.Fatalf("missing result context = %+v", first.Messages)
	}
	mustCommit(t, storage, contextEntry(12, root, nil, []ai.Message{toolResult("c1", "echo", "one", 2)}, nil))
	paired := read()
	if len(paired.Messages) != 2 || paired.Messages[1].(*ai.ToolResultMessage).Content[0].(ai.TextContent).Text != "one" {
		t.Fatalf("first result extension = %+v", paired.Messages)
	}
	mustCommit(t, storage, contextEntry(14, root, nil, []ai.Message{assistantWithCall("c2", "echo", 3)}, nil))
	secondCall := read()
	if len(secondCall.Messages) != 4 || secondCall.Messages[0].(*ai.AssistantMessage).Content[0].(ai.ToolCall).ID != "c1" ||
		secondCall.Messages[1].(*ai.ToolResultMessage).Content[0].(ai.TextContent).Text != "one" ||
		secondCall.Messages[2].(*ai.AssistantMessage).Content[0].(ai.ToolCall).ID != "c2" ||
		secondCall.Messages[3].(*ai.ToolResultMessage).ToolCallID != "c2" {
		t.Fatalf("second assistant extension = %+v", secondCall.Messages)
	}
	mustCommit(t, storage, contextEntry(16, root, nil, []ai.Message{toolResult("c2", "echo", "two", 4)}, nil))
	secondPaired := read()
	if len(secondPaired.Messages) != 4 || secondPaired.Messages[3].(*ai.ToolResultMessage).Content[0].(ai.TextContent).Text != "two" {
		t.Fatalf("second result extension = %+v", secondPaired.Messages)
	}
}

// Port of per-conversation range reuse from packages/durable/src/harness/scheduler.ts at pi v1.1.0 commit da866ada1.
func TestContextRangeIsReusedAcrossTaskInvocations(t *testing.T) {
	base := NewMemoryStorage()
	storage := &contextScanStorage{Storage: base}
	session := NewSessionWithClock(storage, func() int64 { return 1_000 })
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for id := Id(10); id < 30; id++ {
		mustCommit(t, storage, contextEntry(id, RootConversationID, nil, []ai.Message{userMessage("entry", int64(id))}, nil))
	}
	reads := []int{}
	slept := false
	retentionMs := 0
	definition := TaskDefinition{
		Name: "test.shared-context-range", Version: 1,
		Initial: func(json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{"phase":"run"}`), nil },
		Phases: map[string]PhaseHandler{
			"run": func(_ RunningTask, runtime TaskRuntime, taskContext chord.Context) error {
				before := len(storage.calls)
				if _, err := runtime.Context(RootConversationID, taskContext); err != nil {
					return err
				}
				reads = append(reads, len(storage.calls)-before)
				if !slept {
					slept = true
					if err := runtime.Sleep(runtime.Now(), taskContext); err != nil {
						return err
					}
					before = len(storage.calls)
					if _, err := runtime.Context(RootConversationID, taskContext); err != nil {
						return err
					}
					reads = append(reads, len(storage.calls)-before)
				}
				return runtime.Commit(func(tx *Transaction, _ RunningTask) (*NextTaskState, error) {
					return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeCompleted}}, nil
				}, taskContext)
			},
		},
		Abort: func(_ RunningTask, runtime TaskRuntime, taskContext chord.Context) error {
			return runtime.Commit(func(tx *Transaction, _ RunningTask) (*NextTaskState, error) {
				return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeAborted}}, nil
			}, taskContext)
		},
	}
	registry := NewRegistry()
	if err := registry.Install(Extension{Name: "context-range", Tasks: []Task{{Definition: definition}}}); err != nil {
		t.Fatal(err)
	}
	scheduler := NewTaskScheduler(TaskSchedulerOptions{
		Session: session, Storage: storage, Registry: registry, Settings: func() Settings {
			settings := ResolveSettings(nil)
			settings.ContextRetentionMs = retentionMs
			return settings
		},
		Now: func() int64 { return 1_000 }, Context: ctx,
	})
	if err := scheduler.Open(ctx); err != nil {
		t.Fatal(err)
	}
	scheduler.Resume()
	create := func() Id {
		t.Helper()
		var id Id
		if err := session.Commit(ctx, func(tx *Transaction) error {
			var err error
			id, err = tx.CreateTask(definition, json.RawMessage(`{}`), TaskOptions{
				Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: idPointerOf(RootConversationID),
			})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	for index := 0; index < 3; index++ {
		if index == 1 {
			retentionMs = defaultContextRetentionMs
		}
		id := create()
		scheduler.Kick()
		settled, err := scheduler.WaitForTask(ctx, id)
		if err != nil || settled.State.Outcome == nil || settled.State.Outcome.Status != OutcomeCompleted {
			t.Fatalf("task %d settled = %+v, %v", index, settled, err)
		}
		if err := scheduler.WaitForIdle(idPointerOf(RootConversationID), ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(reads) != 4 || reads[0] != 2 || reads[1] != 1 || reads[2] != 2 || reads[3] != 1 {
		t.Fatalf("range scans across sleep and retention modes = %v, want [2 1 2 1]", reads)
	}
	scheduler.Seal()
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

// Port of idle range expiration from packages/durable/src/harness/scheduler.ts at pi v1.1.0 commit da866ada1.
func TestIdleContextRangeExpiresWithoutFurtherTaskChanges(t *testing.T) {
	storage := &contextScanStorage{Storage: NewMemoryStorage()}
	session := NewSession(storage)
	ctx := context.Background()
	if err := session.Commit(ctx, func(tx *Transaction) error {
		_, err := tx.CreateRootConversation()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	mustCommit(t, storage, contextEntry(10, RootConversationID, nil, []ai.Message{userMessage("first", 1)}, nil))
	definition := TaskDefinition{
		Name: "test.expiring-context", Version: 1,
		Initial: func(json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{"phase":"run"}`), nil },
		Phases: map[string]PhaseHandler{
			"run": func(_ RunningTask, runtime TaskRuntime, taskContext chord.Context) error {
				if _, err := runtime.Context(RootConversationID, taskContext); err != nil {
					return err
				}
				return runtime.Commit(func(tx *Transaction, _ RunningTask) (*NextTaskState, error) {
					return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeCompleted}}, nil
				}, taskContext)
			},
		},
		Abort: func(_ RunningTask, runtime TaskRuntime, taskContext chord.Context) error {
			return runtime.Commit(func(tx *Transaction, _ RunningTask) (*NextTaskState, error) {
				return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeAborted}}, nil
			}, taskContext)
		},
	}
	registry := NewRegistry()
	if err := registry.Install(Extension{Name: "expiring-context", Tasks: []Task{{Definition: definition}}}); err != nil {
		t.Fatal(err)
	}
	settings := ResolveSettings(&HarnessSettings{ContextRetentionMs: intPointer(25)})
	scheduler := NewTaskScheduler(TaskSchedulerOptions{
		Session: session, Storage: storage, Registry: registry, Settings: func() Settings { return settings },
		Now: wallClockMillis, Context: ctx,
	})
	if err := scheduler.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		scheduler.Seal()
		if err := session.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	scheduler.Resume()
	var taskID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		taskID, err = tx.CreateTask(definition, json.RawMessage(`{}`), TaskOptions{
			Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: idPointerOf(RootConversationID),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	scheduler.Kick()
	if _, err := scheduler.WaitForTask(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.WaitForIdle(idPointerOf(RootConversationID), ctx); err != nil {
		t.Fatal(err)
	}
	scheduler.mu.Lock()
	_, kept := scheduler.contexts[RootConversationID]
	scheduler.mu.Unlock()
	if !kept {
		t.Fatal("idle conversation did not retain its context range")
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()
	for {
		scheduler.mu.Lock()
		_, kept = scheduler.contexts[RootConversationID]
		scheduler.mu.Unlock()
		if !kept {
			return
		}
		select {
		case <-ticker.C:
		case <-timeout.C:
			t.Fatal("idle context range did not expire")
			return
		}
	}
}

func TestCaptureAndDeriveContext(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()
	root := RootConversationID
	mustCommit(t, storage, conversationWrite(root))
	// e10 (user) -> e11 (head marker pointing at e10) -> e12 (assistant).
	head := Id(10)
	mustCommit(t, storage,
		contextEntry(10, root, nil, []ai.Message{userMessage("one", 1)}, nil),
		contextEntry(11, root, &head, nil, nil),
		contextEntry(12, root, nil, []ai.Message{userMessage("two", 3)}, nil),
	)
	bounds, err := CaptureContextBounds(ctx, storage, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bounds == nil || bounds.Tail != 12 || bounds.Head == nil || bounds.Head.ID != 11 {
		t.Fatalf("bounds = %+v", bounds)
	}
	view, err := DeriveContext(ctx, storage, root, bounds)
	if err != nil {
		t.Fatal(err)
	}
	// The active entries are the head marker followed by the range's non-head
	// entries (the marker's head target is in the range).
	if len(view.Entries) != 3 || view.Entries[0].ID != 11 || view.Entries[1].ID != 10 || view.Entries[2].ID != 12 {
		t.Fatalf("entries = %+v", view.Entries)
	}
	if len(view.Messages) != 2 || view.Messages[0].(*ai.UserMessage).Timestamp != 1 ||
		view.Messages[1].(*ai.UserMessage).Timestamp != 3 {
		t.Fatalf("messages = %+v", view.Messages)
	}
	// Without a head marker the whole range is active.
	mustCommit(t, storage, conversationWrite(50))
	mustCommit(t, storage, contextEntry(100, 50, nil, []ai.Message{userMessage("a", 1)}, nil))
	bounds, err = CaptureContextBounds(ctx, storage, 50, nil)
	if err != nil || bounds.Head != nil {
		t.Fatalf("bounds = %+v, %v", bounds, err)
	}
	view, _ = DeriveContext(ctx, storage, 50, bounds)
	if len(view.Entries) != 1 || view.Entries[0].ID != 100 {
		t.Fatalf("entries = %+v", view.Entries)
	}
	// An empty conversation has no bounds.
	mustCommit(t, storage, conversationWrite(60))
	if bounds, err := CaptureContextBounds(ctx, storage, 60, nil); err != nil || bounds != nil {
		t.Fatalf("empty = %+v, %v", bounds, err)
	}
}

func TestDeriveContextEditsAndStopReasons(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()
	root := RootConversationID
	mustCommit(t, storage, conversationWrite(root))
	replacement := []ai.Message{userMessage("replaced", 9)}
	omitted := Id(12)
	mustCommit(t, storage,
		contextEntry(10, root, nil, []ai.Message{userMessage("one", 1)}, nil),
		contextEntry(11, root, nil, []ai.Message{&ai.AssistantMessage{StopReason: ai.StopAborted, Timestamp: 2}}, nil),
		contextEntry(12, root, nil, []ai.Message{userMessage("three", 3)}, nil),
		contextEntry(13, root, nil, []ai.Message{userMessage("four", 4)}, []ContextEdit{
			{Target: 10, Action: EditReplace, Messages: replacement},
			{Target: omitted, Action: EditOmit},
		}),
	)
	bounds, _ := CaptureContextBounds(ctx, storage, root, nil)
	view, err := DeriveContext(ctx, storage, root, bounds)
	if err != nil {
		t.Fatal(err)
	}
	// e1 is replaced, e2 (aborted) contributes nothing, e3 is omitted, e4 stays.
	if len(view.Contributions) != 4 {
		t.Fatalf("contributions = %+v", view.Contributions)
	}
	if len(view.Contributions[0]) != 1 || view.Contributions[0][0].(*ai.UserMessage).Timestamp != 9 {
		t.Fatalf("replaced = %+v", view.Contributions[0])
	}
	if len(view.Contributions[1]) != 0 || len(view.Contributions[2]) != 0 {
		t.Fatalf("excluded = %+v", view.Contributions)
	}
	if view.Messages[0].(*ai.UserMessage).Timestamp != 9 {
		t.Fatalf("messages = %+v", view.Messages)
	}
}

func TestCaptureContextBoundsRequiresVisibleEntry(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()
	mustCommit(t, storage, conversationWrite(RootConversationID))
	missing := Id(999)
	if _, err := CaptureContextBounds(ctx, storage, RootConversationID, &missing); err == nil {
		t.Fatal("an invisible entry must fail")
	}
}

func TestOrderToolResults(t *testing.T) {
	assistant := assistantWithCall("call-1", "bash", 5)
	result := toolResult("call-1", "bash", "ok", 5)
	user := userMessage("hi", 1)
	ordered := OrderToolResults([]ai.Message{user, assistant, result})
	if len(ordered) != 3 || ordered[0] != ai.Message(user) || ordered[1] != ai.Message(assistant) ||
		ordered[2] != ai.Message(result) {
		t.Fatalf("ordered = %+v", ordered)
	}
	// A missing result is synthesized in call order.
	second := assistantWithCall("call-2", "read", 6)
	ordered = OrderToolResults([]ai.Message{assistant, second, result})
	if len(ordered) != 4 {
		t.Fatalf("ordered = %+v", ordered)
	}
	synthesized, ok := ordered[1].(*ai.ToolResultMessage)
	if !ok || !synthesized.IsError || synthesized.ToolCallID != "call-1" {
		t.Fatalf("synthesized = %+v", ordered[1])
	}
	secondSynth, ok := ordered[3].(*ai.ToolResultMessage)
	if !ok || secondSynth.ToolCallID != "call-2" {
		t.Fatalf("second synthesized = %+v", ordered[3])
	}
	var details map[string]any
	if err := json.Unmarshal(synthesized.Details, &details); err != nil || details["reason"] != "missing_result" {
		t.Fatalf("details = %s", synthesized.Details)
	}
	// An unmatched result is dropped.
	stray := toolResult("call-9", "bash", "stray", 7)
	ordered = OrderToolResults([]ai.Message{user, stray})
	if len(ordered) != 1 || ordered[0] != ai.Message(user) {
		t.Fatalf("ordered = %+v", ordered)
	}
}
