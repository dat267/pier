package durable

import (
	"context"
	"fmt"
	"sort"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/services"
)

// Port of harness/harness.ts: the durable agent harness over one Session.

// EnvTarget is what a harness's environment builder receives.
type EnvTarget struct {
	ConversationID Id
	// Cwd is the conversation's agent cwd.
	Cwd  *string
	Read DocumentReader
}

// HarnessOptions configure an opened Harness.
type HarnessOptions struct {
	// Models is the pi-ai model access used by generation.
	Models *ai.Models
	// Registry is the extension registry, which may keep changing while the
	// harness runs.
	Registry RegistryReader
	Settings *HarnessSettings
	// Env builds a conversation's environment at each use; nil means none.
	Env func(target EnvTarget, ctx chord.Context) (ExecutionEnv, error)
	// ConversationCreated runs in every commit that creates or forks a
	// conversation, after the built-in pi.* documents.
	ConversationCreated func(tx *Transaction, record ConversationRecord) error
	// Now is the harness clock; nil means zero.
	Now func() int64
	// OnReport receives extension failures that do not fail the caller.
	OnReport func(error)
}

// Harness is the durable agent harness.
type Harness struct {
	*Session
	storage     Storage
	options     HarnessOptions
	report      func(error)
	tasks       *TaskScheduler
	submissions *Submissions
	views       *ConversationViews
	taskGraph   *TaskGraphView
	host        *conversationHost
	closed      bool
}

type conversationHost struct {
	harness     *Harness
	storage     Storage
	tasks       *TaskScheduler
	submissions *Submissions
	views       *ConversationViews
	now         func() int64
}

// NewHarness builds a harness over storage; Open must be called before use.
func NewHarness(storage Storage, options HarnessOptions, ctx chord.Context) *Harness {
	session := NewSession(storage)
	harness := &Harness{
		Session: session, storage: storage, options: options,
		report: func(error) {},
	}
	if options.OnReport != nil {
		harness.report = options.OnReport
	}
	now := options.Now
	if now == nil {
		now = func() int64 { return 0 }
	}
	settings := func() Settings { return ResolveSettings(options.Settings) }
	harness.tasks = NewTaskScheduler(TaskSchedulerOptions{
		Session: session, Storage: storage, Registry: options.Registry, Models: options.Models,
		Agent: func(id Id, snapshot RegistrySnapshot, ctx chord.Context) (Agent, error) {
			return harness.ResolveAgent(id, snapshot, ctx)
		},
		Settings: settings,
		Env: func(id Id, ctx chord.Context) (ExecutionEnv, error) {
			return harness.BuildEnv(id, ctx)
		},
		Now:    now,
		Report: harness.report,
		SettleOutcome: func(tx *Transaction, record TaskRecord, outcome *TaskOutcome) error {
			return SettleSchedulerOutcome(tx, record, SchedulerOutcome{Status: outcome.Status, Error: outcome.Error, Reason: outcome.Reason})
		},
		WithdrawInputs: WithdrawQueuedInputs,
		Conversation: func(id Id, invocation *Invocation, ctx chord.Context) (ConversationHandle, bool, error) {
			return harness.BoundConversation(id, invocation, ctx)
		},
		Context: chord.WithoutAbortSignal(ctx),
	})
	harness.submissions = NewSubmissions(session, storage, now, func() QueueModes {
		resolved := settings()
		return QueueModes{SteeringMode: resolved.SteeringMode, FollowUpMode: resolved.FollowUpMode}
	}, func() { harness.tasks.Resume() })
	harness.views = NewConversationViews(session, storage)
	harness.taskGraph = NewTaskGraphView(session, storage)
	harness.host = &conversationHost{
		harness: harness, storage: storage, tasks: harness.tasks, submissions: harness.submissions,
		views: harness.views, now: now,
	}
	session.ConversationCreatedHook = func(tx *Transaction, record ConversationRecord) error {
		if _, err := tx.Doc(LiveDoc.Definition, record.ID); err != nil {
			return err
		}
		if _, err := tx.Doc(InboxDoc.Definition, record.ID); err != nil {
			return err
		}
		if _, err := tx.Doc(UsageDoc.Definition, record.ID); err != nil {
			return err
		}
		if err := CreateAgent(tx, record); err != nil {
			return err
		}
		if options.ConversationCreated != nil {
			return options.ConversationCreated(tx, record)
		}
		return nil
	}
	session.BeforeCloseHook = func() error {
		return harness.tasks.Join(ctx)
	}
	return harness
}

// OpenHarness opens a harness: the registry must hold the built-in tasks.
func OpenHarness(storage Storage, options HarnessOptions, ctx chord.Context) (*Harness, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if options.Registry != nil {
		snapshot := options.Registry.Snapshot()
		for _, task := range BuiltinTaskDefinitions() {
			if snapshot.Task(task.Definition.Name) == nil {
				return nil, fmt.Errorf("Registry lacks built-in tasks %s; create it with NewRegistry()", task.Definition.Name)
			}
		}
	}
	harness := NewHarness(storage, options, ctx)
	if err := harness.tasks.Open(ctx); err != nil {
		_ = harness.Session.Close(chord.WithoutAbortSignal(ctx))
		return nil, err
	}
	return harness, nil
}

// ResolveAgent resolves a conversation's committed pi.agent against snapshot.
func (h *Harness) ResolveAgent(id Id, snapshot RegistrySnapshot, ctx chord.Context) (Agent, error) {
	var registry RegistrySnapshot
	if snapshot != nil {
		registry = snapshot
	} else if h.options.Registry != nil {
		registry = h.options.Registry.Snapshot()
	}
	value, present, err := h.Session.Snapshot(ctx, AgentDoc.Definition, id)
	if err != nil {
		return Agent{}, err
	}
	state := agentStateOf(value, present)
	return ResolveAgent(state, registry, ResolveSettings(h.options.Settings), h.report)
}

func agentStateOf(value chord.JsonValue, present bool) *AgentState {
	if !present || value == nil {
		return nil
	}
	state, ok := decodeJSONInto[AgentState](value)
	if !ok {
		return nil
	}
	return &state
}

// BuildEnv builds a conversation's environment from its current cwd.
func (h *Harness) BuildEnv(id Id, ctx chord.Context) (ExecutionEnv, error) {
	if h.options.Env == nil {
		return nil, nil
	}
	value, present, err := h.Session.Snapshot(ctx, AgentDoc.Definition, id)
	if err != nil {
		return nil, err
	}
	target := EnvTarget{ConversationID: id, Read: h.Session}
	if state := agentStateOf(value, present); state != nil {
		target.Cwd = state.Cwd
	}
	return h.options.Env(target, ctx)
}

// Resume enables task scheduling.
func (h *Harness) Resume() { h.tasks.Resume() }

// GetTask reads a committed task.
func (h *Harness) GetTask(ctx chord.Context, id Id) (*TaskRecord, error) {
	return h.tasks.GetTask(ctx, id)
}

// Inspect derives the point-in-time view of live work.
func (h *Harness) Inspect(ctx chord.Context) (HarnessInspection, error) {
	var snapshot RegistrySnapshot
	if h.options.Registry != nil {
		snapshot = h.options.Registry.Snapshot()
	}
	inspection := h.tasks.Inspect(snapshot)
	scan := func(status string) ([]SubmissionRecord, error) {
		statusValue := status
		return ScanAll(func(cursor Cursor) (Page[SubmissionRecord], error) {
			return h.storage.ScanSubmissions(ctx, SubmissionQuery{Status: &statusValue}, cursor, 256)
		})
	}
	queued, err := scan(SubmissionQueued)
	if err != nil {
		return HarnessInspection{}, err
	}
	placed, err := scan(SubmissionPlaced)
	if err != nil {
		return HarnessInspection{}, err
	}
	submissions := append(queued, placed...)
	sort.Slice(submissions, func(left, right int) bool { return submissions[left].ID < submissions[right].ID })
	inspection.Submissions = submissions
	return inspection, nil
}

// Submission returns the handle of an existing submission, or nil.
func (h *Harness) Submission(id Id, ctx chord.Context) (Submission, error) {
	return h.submissions.Get(id, ctx)
}

// AbortSubmission withdraws a submission.
func (h *Harness) AbortSubmission(id Id, ctx chord.Context, conversationID *Id) (string, error) {
	return h.submissions.Abort(id, ctx, conversationID)
}

// AbortTask marks or settles a task.
func (h *Harness) AbortTask(id Id, ctx chord.Context) (string, error) {
	return h.tasks.Abort(id, ctx)
}

// WaitForTask resolves with the task's terminal receipt.
func (h *Harness) WaitForTask(id Id, ctx chord.Context) (SettledTask, error) {
	h.tasks.Resume()
	return h.tasks.WaitForTask(ctx, id)
}

// WaitForIdle resolves when every ownerless scope is idle.
func (h *Harness) WaitForIdle(ctx chord.Context) error {
	h.tasks.Resume()
	return h.tasks.WaitForIdle(nil, ctx)
}

// Usage sums every conversation's committed pi.usage.
func (h *Harness) Usage(ctx chord.Context) (UsageState, error) {
	total := UsageState{Models: map[string]ai.Usage{}, Tools: map[string]ai.Usage{}}
	conversations, err := ScanAll(func(cursor Cursor) (Page[ConversationRecord], error) {
		return h.storage.ScanConversations(ctx, ConversationQuery{}, cursor, 256)
	})
	if err != nil {
		return UsageState{}, err
	}
	for _, conversation := range conversations {
		value, present, err := h.Session.Snapshot(ctx, UsageDoc.Definition, conversation.ID)
		if err != nil {
			return UsageState{}, err
		}
		if !present {
			continue
		}
		if state, ok := decodeJSONInto[UsageState](value); ok {
			AddUsageState(&total, state)
		}
	}
	return total, nil
}

// TaskGraph is the structural task graph as an attached state.
func (h *Harness) TaskGraph(ctx chord.Context) (*services.AttachedState[TaskGraph], error) {
	state, _, err := h.taskGraph.State(ctx)
	return state, err
}

// WatchTaskGraph is the task graph as an exact-frame watch.
func (h *Harness) WatchTaskGraph(ctx chord.Context) (TaskGraphWatch, error) {
	watch, _, err := h.taskGraph.Watch(ctx)
	return watch, err
}

// Root returns the reserved root conversation, creating it when absent.
func (h *Harness) Root(ctx chord.Context, options *ConversationCreateOptions) (*HarnessConversation, error) {
	return h.create(createTarget{kind: "root"}, createOptionsOf(options), ctx)
}

// Conversation returns the handle of an existing conversation, or nil.
func (h *Harness) Conversation(ctx chord.Context, id Id) (*HarnessConversation, error) {
	if h.closed {
		return nil, ClosedError()
	}
	record, err := h.storage.Conversation(ctx, id)
	if err != nil || record == nil {
		return nil, err
	}
	return &HarnessConversation{id: record.ID, host: h.host}, nil
}

// CreateConversation creates a conversation with explicit ownership.
func (h *Harness) CreateConversation(ctx chord.Context, options ConversationCreateOptions) (*HarnessConversation, error) {
	return h.create(createTarget{kind: "independent", ownership: options.Ownership}, createOptionsOf(&options), ctx)
}

// Close seals the harness and closes storage.
func (h *Harness) Close(ctx chord.Context) error {
	h.closed = true
	return h.Session.Close(ctx)
}

type createTarget struct {
	kind      string
	parentID  Id
	at        Id
	ownership ConversationOwnership
}

type createOptions struct {
	agent *AgentChange
	init  func(tx *Transaction, conversationID Id) error
}

func createOptionsOf(options *ConversationCreateOptions) createOptions {
	if options == nil {
		return createOptions{}
	}
	return createOptions{agent: options.Agent, init: options.Init}
}

func (h *Harness) create(target createTarget, options createOptions, ctx chord.Context) (*HarnessConversation, error) {
	if h.closed {
		return nil, ClosedError()
	}
	var id Id
	err := h.Session.Commit(ctx, func(tx *Transaction) error {
		if target.kind == "root" {
			if existing, err := tx.Conversation(RootConversationID); err != nil {
				return err
			} else if existing != nil {
				id = RootConversationID
				return nil
			}
		}
		var record *ConversationRecord
		var err error
		switch target.kind {
		case "root":
			record, err = tx.CreateRootConversation()
		case "fork":
			record, err = tx.ForkConversation(target.parentID, target.at, target.ownership)
		default:
			record, err = tx.CreateConversation(target.ownership)
		}
		if err != nil {
			return err
		}
		if options.agent != nil {
			if err := Configure(tx, record.ID, *options.agent); err != nil {
				return err
			}
		}
		if options.init != nil {
			if err := options.init(tx, record.ID); err != nil {
				return err
			}
		}
		id = record.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &HarnessConversation{id: id, host: h.host}, nil
}

// BoundConversation is an invocation-bound conversation handle.
func (h *Harness) BoundConversation(id Id, invocation *Invocation, ctx chord.Context) (ConversationHandle, bool, error) {
	record, err := h.storage.Conversation(ctx, id)
	if err != nil || record == nil {
		return nil, false, err
	}
	return &invocationConversation{
		id: id, invocation: invocation, submissions: h.submissions, tasks: h.tasks,
	}, true, nil
}

// HarnessConversation is a stateless handle of one conversation.
type HarnessConversation struct {
	id   Id
	host *conversationHost
}

// ID is the conversation id.
func (c *HarnessConversation) ID() Id { return c.id }

// Agent resolves the conversation's agent.
func (c *HarnessConversation) Agent(ctx chord.Context) (Agent, error) {
	return c.host.harness.ResolveAgent(c.id, nil, ctx)
}

// Configure applies an agent change in its own commit.
func (c *HarnessConversation) Configure(ctx chord.Context, change AgentChange) error {
	return c.host.harness.Session.Commit(ctx, func(tx *Transaction) error {
		return Configure(tx, c.id, change)
	})
}

// Submit admits user input or a passive write.
func (c *HarnessConversation) Submit(ctx chord.Context, draft SubmissionDraft) (Submission, error) {
	return c.host.submissions.Submit(c.id, draft, ctx)
}

// Compact admits a manual compaction task.
func (c *HarnessConversation) Compact(ctx chord.Context, instructions *string) (Id, error) {
	c.host.tasks.Resume()
	input := CompactionInput{Reason: CompactionManual, Instructions: instructions}
	var id Id
	err := c.host.harness.Session.Commit(ctx, func(tx *Transaction) error {
		var err error
		id, err = CreateCompaction(tx, c.id, input, nil)
		return err
	})
	return id, err
}

// Reset admits a pi.reset write that starts a new context.
func (c *HarnessConversation) Reset(ctx chord.Context, handoff *string) error {
	entry := EntryDraft{Kind: ResetEntry.Kind, HeadSelf: true}
	if handoff != nil {
		message, err := userMessageFromContent(*handoff, c.host.now())
		if err != nil {
			return err
		}
		entry.Model = []ai.Message{message}
	}
	_, err := c.host.submissions.Submit(c.id, SubmissionDraft{Type: SubmissionTypeWrite, Entry: &entry}, ctx)
	return err
}

// Commit runs one transaction scoped to the conversation.
func (c *HarnessConversation) Commit(ctx chord.Context, change func(tx *Transaction) error) error {
	scope := TransactionScope{ConversationID: &c.id}
	return c.host.harness.Session.CommitWith(ctx, change, scope)
}

// Context derives the committed transcript and model context.
func (c *HarnessConversation) Context(ctx chord.Context) (ContextView, error) {
	return ReadContext(ctx, c.host.harness.Session, c.host.storage, c.id, nil)
}

// Entries scans the conversation's visible history newest-first.
func (c *HarnessConversation) Entries(ctx chord.Context, query EntryQuery, limit int, cursor Cursor) (Page[EntryRecord], error) {
	query.ConversationID = c.id
	var page Page[EntryRecord]
	err := c.host.harness.Session.ReadOnLine(func() error {
		var err error
		page, err = c.host.storage.ScanEntries(ctx, query, cursor, limit)
		return err
	})
	return page, err
}

// Fork creates a conversation forked from this one at an entry.
func (c *HarnessConversation) Fork(ctx chord.Context, at Id, options ConversationCreateOptions) (*HarnessConversation, error) {
	target := createTarget{kind: "fork", parentID: c.id, at: at, ownership: options.Ownership}
	return c.host.harness.create(target, createOptionsOf(&options), ctx)
}

// Abort withdraws the queued inputs and marks every reachable live task.
func (c *HarnessConversation) Abort(ctx chord.Context, background bool) error {
	c.host.tasks.Resume()
	return c.host.tasks.AbortConversation(c.id, background, ctx)
}

// WaitForIdle resolves when the conversation's scope is idle.
func (c *HarnessConversation) WaitForIdle(ctx chord.Context) error {
	c.host.tasks.Resume()
	return c.host.tasks.WaitForIdle(&c.id, ctx)
}

// ViewState is the structural view as an attached state.
func (c *HarnessConversation) ViewState(ctx chord.Context) (*services.AttachedState[ConversationView], error) {
	return c.host.views.State(c.id, ctx)
}

// Watch is the structural view as an exact-frame watch.
func (c *HarnessConversation) Watch(ctx chord.Context) (WatchHandle[ConversationView], error) {
	return c.host.views.Watch(c.id, ctx)
}

// invocationConversation binds a conversation to a live task invocation.
type invocationConversation struct {
	id          Id
	invocation  *Invocation
	submissions *Submissions
	tasks       *TaskScheduler
}

func (c *invocationConversation) ConversationID() Id { return c.id }

func (c *invocationConversation) bind(ctx chord.Context) chord.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	combined, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.invocation.Context(), cancel)
	_ = stop
	return combined
}

func (c *invocationConversation) Submit(draft InputSubmissionDraft, ctx chord.Context) (Submission, error) {
	if err := c.invocation.AssertLive(); err != nil {
		return nil, err
	}
	return c.submissions.Submit(c.id, SubmissionDraft{
		RequestID: draft.RequestID, Type: SubmissionTypeInput, Content: draft.Content, WhenBusy: draft.WhenBusy,
	}, c.bind(ctx))
}

func (c *invocationConversation) Abort(ctx chord.Context, options *ConversationAbortOptions) error {
	if err := c.invocation.AssertLive(); err != nil {
		return err
	}
	background := options != nil && options.Background
	return c.tasks.AbortConversation(c.id, background, c.bind(ctx))
}

func (c *invocationConversation) WaitForIdle(ctx chord.Context) error {
	if err := c.invocation.AssertLive(); err != nil {
		return err
	}
	return c.tasks.WaitForIdle(&c.id, c.bind(ctx))
}
