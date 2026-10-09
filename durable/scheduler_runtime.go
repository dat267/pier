package durable

import (
	"context"
	"fmt"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of `#runtime` in harness/scheduler.ts: the operation set one task
// invocation sees.

// runtimePhase is what a runtime serves for its phase: the phase's snapshot and
// task, and its lazily resolved agent.
type runtimePhase struct {
	snapshot func() RegistrySnapshot
	task     func() Task
	agent    Agent
	agentSet bool
}

type schedulerRuntime struct {
	scheduler  *TaskScheduler
	invocation *Invocation
	phase      *runtimePhase
}

func (r *schedulerRuntime) TaskID() Id         { return r.invocation.TaskID }
func (r *schedulerRuntime) ConversationID() Id { return r.invocation.ConversationID }
func (r *schedulerRuntime) Signal() context.Context {
	return r.invocation.Context()
}

func (r *schedulerRuntime) Registry() RegistrySnapshot {
	if r.phase.snapshot == nil {
		return nil
	}
	return r.phase.snapshot()
}

func (r *schedulerRuntime) Models() *ai.Models { return r.scheduler.models }

func (r *schedulerRuntime) Settings() Settings {
	if r.scheduler.settings == nil {
		return ResolveSettings(nil)
	}
	return r.scheduler.settings()
}

func (r *schedulerRuntime) Agent(ctx chord.Context) (Agent, error) {
	if err := r.invocation.AssertLive(); err != nil {
		return Agent{}, err
	}
	if !r.phase.agentSet {
		if r.scheduler.agent == nil {
			r.phase.agent = Agent{}
		} else {
			resolved, err := r.scheduler.agent(r.invocation.ConversationID, r.Registry(), r.invocation.Context())
			if err != nil {
				return Agent{}, err
			}
			r.phase.agent = resolved
		}
		r.phase.agentSet = true
	}
	return r.phase.agent, nil
}

func (r *schedulerRuntime) Env(ctx chord.Context) (ExecutionEnv, error) {
	if err := r.invocation.AssertLive(); err != nil {
		return nil, err
	}
	if r.scheduler.env == nil {
		return nil, nil
	}
	return r.scheduler.env(r.invocation.ConversationID, ctx)
}

func (r *schedulerRuntime) Hooks() HookRunner {
	agent, _ := r.Agent(context.Background())
	taskName := ""
	if r.phase.task != nil {
		taskName = r.phase.task().Definition.Name
	}
	return &schedulerHooks{handlers: AgentHooks(agent, taskName), report: r.scheduler.report, invocation: r.invocation}
}

func (r *schedulerRuntime) Commit(change func(tx *Transaction, current RunningTask) (*NextTaskState, error), ctx chord.Context) error {
	if err := r.invocation.AssertLive(); err != nil {
		return err
	}
	return r.scheduler.session.Commit(ctx, func(tx *Transaction) error {
		if err := r.invocation.AssertLive(); err != nil {
			return err
		}
		if r.scheduler.Closing() {
			return ClosedError()
		}
		r.scheduler.mu.Lock()
		found, present := r.scheduler.mirror.Live[r.invocation.TaskID]
		r.scheduler.mu.Unlock()
		if !present {
			return fmt.Errorf("Task %d is terminal", r.invocation.TaskID)
		}
		if found.State.Status != TaskRunning {
			return fmt.Errorf("Task %d is %s", r.invocation.TaskID, found.State.Status)
		}
		if r.invocation.Mode == "run" && found.AbortRequested {
			return fmt.Errorf("Task %d has a durable abort mark", r.invocation.TaskID)
		}
		next, err := change(tx, RunningTask{found})
		if err != nil {
			return err
		}
		if next == nil {
			return nil
		}
		return CommitTaskState(tx, r.scheduler.LiveRecords(), found, *next, nil)
	})
}

func (r *schedulerRuntime) Memo(ctx chord.Context, name string) (chord.JsonValue, bool, error) {
	if err := r.invocation.AssertLive(); err != nil {
		return nil, false, err
	}
	r.scheduler.mu.Lock()
	record, present := r.scheduler.mirror.Live[r.invocation.TaskID]
	r.scheduler.mu.Unlock()
	if !present {
		return nil, false, nil
	}
	raw, ok := MemoOf(&record, name)
	if !ok {
		return nil, false, nil
	}
	return decodeJSONValue(raw), true, nil
}

func (r *schedulerRuntime) MemoSet(ctx chord.Context, name string, candidate chord.JsonValue) (chord.JsonValue, error) {
	if err := r.invocation.AssertLive(); err != nil {
		return nil, err
	}
	encoded, err := marshalJSONValue(candidate)
	if err != nil {
		return nil, err
	}
	raw := jsonRaw(encoded)
	var winner chord.JsonValue
	err = r.scheduler.session.Commit(ctx, func(tx *Transaction) error {
		r.scheduler.mu.Lock()
		record, present := r.scheduler.mirror.Live[r.invocation.TaskID]
		r.scheduler.mu.Unlock()
		if !present {
			return fmt.Errorf("Task %d is terminal", r.invocation.TaskID)
		}
		winnerRaw, err := MemoSet(tx, record, name, raw)
		if err != nil {
			return err
		}
		winner = decodeJSONValue(winnerRaw)
		return nil
	})
	return winner, err
}

func (r *schedulerRuntime) GetTask(id Id, ctx chord.Context) (*TaskRecord, error) {
	if err := r.invocation.AssertLive(); err != nil {
		return nil, err
	}
	return r.scheduler.GetTask(ctx, id)
}

func (r *schedulerRuntime) WaitForTask(id Id, ctx chord.Context) (SettledTask, error) {
	if err := r.invocation.AssertLive(); err != nil {
		return SettledTask{}, err
	}
	return r.scheduler.WaitForTask(ctx, id)
}

func (r *schedulerRuntime) Outcomes(ids []Id, ctx chord.Context) ([]TaskOutcome, error) {
	outcomes := make([]TaskOutcome, 0, len(ids))
	for _, id := range ids {
		record, err := r.GetTask(id, ctx)
		if err != nil {
			return nil, err
		}
		if record == nil || record.State.Status != TaskTerminal || record.State.Outcome == nil {
			return nil, fmt.Errorf("Task %d does not exist or is not terminal", id)
		}
		outcomes = append(outcomes, *record.State.Outcome)
	}
	return outcomes, nil
}

func (r *schedulerRuntime) Conversation(id Id, ctx chord.Context) (ConversationHandle, bool, error) {
	if err := r.invocation.AssertLive(); err != nil {
		return nil, false, err
	}
	if r.scheduler.conversation == nil {
		return nil, false, nil
	}
	return r.scheduler.conversation(id, r.invocation, ctx)
}

func (r *schedulerRuntime) Entry(ctx chord.Context, id Id) (*EntryRecord, error) {
	if err := r.invocation.AssertLive(); err != nil {
		return nil, err
	}
	commit, err := r.scheduler.storage.Entry(ctx, id)
	if err != nil || commit == nil {
		return nil, err
	}
	return &commit.Entry, nil
}

func (r *schedulerRuntime) Context(conversationID Id, ctx chord.Context, options ...ConversationContextOptions) (ContextView, error) {
	var at *Id
	if len(options) > 0 {
		at = options[0].At
	}
	if err := r.invocation.AssertLive(); err != nil {
		return ContextView{}, err
	}
	previous := r.scheduler.contextRange(conversationID)
	if err := r.invocation.AssertLive(); err != nil {
		return ContextView{}, err
	}
	view, cached, err := readContextFrom(ctx, r.scheduler.session, r.scheduler.storage,
		conversationID, at, previous)
	if err != nil {
		return ContextView{}, err
	}
	if !r.invocation.Ended() {
		r.scheduler.keepContextRange(conversationID, cached)
	}
	return view, nil
}

func (r *schedulerRuntime) Now() int64 {
	if r.invocation.Ended() {
		panic(&EndedInvocationError{TaskID: r.invocation.TaskID})
	}
	if r.scheduler.now == nil {
		return 0
	}
	return r.scheduler.now()
}

func (r *schedulerRuntime) Report(err error) {
	if r.invocation.Ended() {
		panic(&EndedInvocationError{TaskID: r.invocation.TaskID})
	}
	if r.scheduler.report != nil {
		r.scheduler.report(err)
	}
}

func (r *schedulerRuntime) Sleep(until int64, ctx chord.Context) error {
	if err := r.invocation.AssertLive(); err != nil {
		return err
	}
	signals := []context.Context{r.invocation.Context()}
	if ctx != nil && ctx.Done() != nil {
		signals = append(signals, ctx)
	}
	combined, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, signal := range signals {
		go func(signal context.Context) {
			select {
			case <-signal.Done():
				cancel()
			case <-combined.Done():
			}
		}(signal)
	}
	for {
		if err := combined.Err(); err != nil {
			return err
		}
		remaining := until - r.scheduler.now()
		if remaining <= 0 {
			return nil
		}
		if err := Delay(remaining, combined); err != nil {
			return err
		}
	}
}

func (r *schedulerRuntime) Snapshot(ctx chord.Context, definition DocDefinition, args ...any) (chord.JsonValue, bool, error) {
	if err := r.invocation.AssertLive(); err != nil {
		return nil, false, err
	}
	return r.scheduler.session.Snapshot(ctx, definition, args...)
}

func (r *schedulerRuntime) SnapshotAsOf(ctx chord.Context, definition DocDefinition, conversationID Id, at Id) (chord.JsonValue, bool, error) {
	if err := r.invocation.AssertLive(); err != nil {
		return nil, false, err
	}
	return r.scheduler.session.SnapshotAsOf(ctx, definition, conversationID, at)
}

func (r *schedulerRuntime) WatchDoc(ctx chord.Context, definition DocDefinition, args ...any) (DocumentWatch[chord.JsonValue], bool, error) {
	if err := r.invocation.AssertLive(); err != nil {
		return nil, false, err
	}
	watch, present, err := r.scheduler.session.WatchDoc(ctx, definition, args...)
	if err != nil || !present {
		return watch, present, err
	}
	r.invocation.AddWatch(&documentWatchStopper{watch: watch})
	return watch, true, nil
}

// documentWatchStopper adapts a document watch to the invocation's stopper.
type documentWatchStopper struct {
	watch DocumentWatch[chord.JsonValue]
}

func (s *documentWatchStopper) Stop() WatchEnd { return s.watch.Stop() }

// schedulerHooks dispatches one hook of a task to every matching registered
// handler, in extension order.
type schedulerHooks struct {
	handlers   []any
	report     func(error)
	invocation *Invocation
}

func (h *schedulerHooks) Each(name string, invoke func(handler any) error) error {
	for _, handler := range h.handlers {
		if h.invocation.Ended() {
			return &EndedInvocationError{TaskID: h.invocation.TaskID}
		}
		if err := invoke(handler); err != nil {
			if h.invocation.Ended() {
				return err
			}
			if h.report != nil {
				h.report(err)
			}
			continue
		}
	}
	return nil
}

// aiModels is the pi-ai model access type (the ai package's Models).
type aiModels = ai.Models

// jsonRaw is a marshaled JSON value as a raw message.
func jsonRaw(encoded string) []byte { return []byte(encoded) }
