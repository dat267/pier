package durable

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"sync"

	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
	"github.com/dat267/pier/chord/services"
)

// Port of harness/task-graph.ts: the Session's live-task graph, mounted by its
// first observer and advanced from commit publications.

// TaskGraphState is a live task's durable status without its checkpoint and
// outcome payloads.
type TaskGraphState struct {
	// Status is pending, running, waiting or completing.
	Status string `json:"status"`
	// Phase is the checkpoint's phase.
	Phase string `json:"phase,omitempty"`
	// On and Policy describe a waiting task.
	On     []Id   `json:"on,omitempty"`
	Policy string `json:"policy,omitempty"`
	// Outcome is the terminal status a completing task holds.
	Outcome string `json:"outcome,omitempty"`
}

// TaskGraphNode is one live task in the graph.
type TaskGraphNode struct {
	ID             Id     `json:"id"`
	Kind           string `json:"kind"`
	ConversationID Id     `json:"conversationId"`
	// Owner is absent for a conversation-owned task.
	Owner          *Id            `json:"owner,omitempty"`
	Background     bool           `json:"background"`
	AbortRequested bool           `json:"abortRequested"`
	State          TaskGraphState `json:"state"`
	// Conversations are the conversations this task owns, in id order.
	Conversations []Id `json:"conversations"`
}

// TaskGraph is every live task of the session, keyed by its decimal id.
type TaskGraph struct {
	Tasks map[string]TaskGraphNode `json:"tasks"`
}

// TaskGraphWatch observes the graph frame by frame.
type TaskGraphWatch = WatchHandle[TaskGraph]

// live task statuses (upstream LIVE_STATUSES).
var liveTaskStatuses = []string{TaskPending, TaskRunning, TaskWaiting, TaskCompleting}

// scan page size for the mount build.
const taskGraphScanPageSize = 256

// TaskGraphView is the harness's task graph mount.
type TaskGraphView struct {
	session *Session
	storage Storage

	mu     sync.Mutex
	mount  *taskGraphMount
	closed bool
}

type taskGraphMount struct {
	value     TaskGraph
	observers map[any]struct{}
}

// NewTaskGraphView builds the view and subscribes it to the session.
func NewTaskGraphView(session *Session, storage Storage) *TaskGraphView {
	view := &TaskGraphView{session: session, storage: storage}
	session.SubscribeCommits(func(publication CommitPublication, ctx chord.Context) {
		view.mu.Lock()
		mount := view.mount
		view.mu.Unlock()
		if mount != nil {
			view.advance(mount, publication, ctx)
		}
	})
	session.SubscribeClose(func() {
		view.mu.Lock()
		view.closed = true
		observers := make([]any, 0)
		if view.mount != nil {
			for observer := range view.mount.observers {
				observers = append(observers, observer)
			}
		}
		view.mount = nil
		view.mu.Unlock()
		for _, observer := range observers {
			closeObserver(observer)
		}
	})
	return view
}

// State returns a disposable read-only replicated state of the graph.
func (v *TaskGraphView) State(ctx chord.Context) (*services.AttachedState[TaskGraph], func(), error) {
	var source *CommittedStateSource[TaskGraph]
	var detach func()
	err := v.session.ReadOnLine(func() error {
		mount, err := v.mountLocked()
		if err != nil {
			return err
		}
		var innerDetach func()
		source, innerDetach = v.attachState(mount)
		detach = innerDetach
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return services.NewAttachedState[TaskGraph](source, nil), detach, nil
}

// Watch returns a serialized exact-frame watch of the graph; cancelling the
// context stops it.
func (v *TaskGraphView) Watch(ctx chord.Context) (TaskGraphWatch, func(), error) {
	var watch *CommittedWatch[TaskGraph]
	var detach func()
	err := v.session.ReadOnLine(func() error {
		mount, err := v.mountLocked()
		if err != nil {
			return err
		}
		var innerDetach func()
		watch, innerDetach = v.attachWatch(mount)
		detach = innerDetach
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if ctx != nil && ctx.Done() != nil {
		watch.ObserveCancellation(ctx)
	}
	return watch, detach, nil
}

// mountLocked returns the existing mount or builds it (the session line is
// held).
func (v *TaskGraphView) mountLocked() (*taskGraphMount, error) {
	if v.mount != nil {
		return v.mount, nil
	}
	value, err := v.build(context.Background())
	if err != nil {
		return nil, err
	}
	v.mount = &taskGraphMount{value: value, observers: map[any]struct{}{}}
	return v.mount, nil
}

func (v *TaskGraphView) attachState(mount *taskGraphMount) (*CommittedStateSource[TaskGraph], func()) {
	var source *CommittedStateSource[TaskGraph]
	detach := func() {
		v.mu.Lock()
		delete(mount.observers, source)
		if len(mount.observers) == 0 && v.mount == mount {
			v.mount = nil
		}
		v.mu.Unlock()
	}
	source = NewCommittedStateSource[TaskGraph](mount.value, detach)
	mount.observers[source] = struct{}{}
	return source, detach
}

func (v *TaskGraphView) attachWatch(mount *taskGraphMount) (*CommittedWatch[TaskGraph], func()) {
	var watch *CommittedWatch[TaskGraph]
	detach := func() {
		v.mu.Lock()
		delete(mount.observers, watch)
		if len(mount.observers) == 0 && v.mount == mount {
			v.mount = nil
		}
		v.mu.Unlock()
	}
	watch = NewCommittedWatch[TaskGraph](mount.value, detach, nil)
	mount.observers[watch] = struct{}{}
	return watch, detach
}

func (v *TaskGraphView) build(ctx chord.Context) (TaskGraph, error) {
	tasks := map[string]TaskGraphNode{}
	var records []TaskRecord
	for _, status := range liveTaskStatuses {
		statusValue := status
		page, err := ScanAll(func(cursor Cursor) (Page[TaskRecord], error) {
			return v.storage.ScanTasks(ctx, TaskQuery{Status: &statusValue}, cursor, taskGraphScanPageSize)
		})
		if err != nil {
			return TaskGraph{}, err
		}
		records = append(records, page...)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	for _, record := range records {
		taskID := record.ID
		owned, err := ScanAll(func(cursor Cursor) (Page[ConversationRecord], error) {
			return v.storage.ScanConversations(ctx, ConversationQuery{OwnerTaskID: &taskID}, cursor, taskGraphScanPageSize)
		})
		if err != nil {
			return TaskGraph{}, err
		}
		conversations := make([]Id, 0, len(owned))
		for _, conversation := range owned {
			conversations = append(conversations, conversation.ID)
		}
		sort.Slice(conversations, func(i, j int) bool { return conversations[i] < conversations[j] })
		tasks[strconv.FormatInt(int64(record.ID), 10)] = taskGraphNode(record, conversations)
	}
	return TaskGraph{Tasks: tasks}, nil
}

// advance applies one publication to the mount and hands the revision to every
// observer.
func (v *TaskGraphView) advance(mount *taskGraphMount, publication CommitPublication, ctx chord.Context) {
	ops := []delta.Op{}
	changed := map[string]*TaskGraphNode{}
	node := func(key string) *TaskGraphNode {
		if value, present := changed[key]; present {
			return value
		}
		if value, present := mount.value.Tasks[key]; present {
			copied := value
			return &copied
		}
		return nil
	}
	for _, change := range publication.Changes {
		if change.Write == nil || change.Write.Type != "task" || change.Write.Task == nil {
			continue
		}
		record := change.Write.Task
		key := strconv.FormatInt(int64(record.ID), 10)
		previous := node(key)
		if record.State.Status == TaskTerminal {
			if previous == nil {
				continue
			}
			ops = append(ops, delta.Op{Verb: delta.VerbDelete, Path: delta.Path{"tasks", key}})
			changed[key] = nil
			continue
		}
		conversations := []Id{}
		if previous != nil {
			conversations = previous.Conversations
		}
		next := taskGraphNode(*record, conversations)
		if previous != nil && nodesJSONEqual(next, *previous) {
			continue
		}
		ops = append(ops, delta.Op{Verb: delta.VerbSet, Path: delta.Path{"tasks", key}, Value: nodeValue(next)})
		copied := next
		changed[key] = &copied
	}
	// After the tasks, so a conversation created with its owner task in one
	// commit finds the owner's node.
	created := map[string][]Id{}
	for _, change := range publication.Changes {
		if change.Write == nil || change.Write.Type != "conversation" || change.Write.Conversation == nil {
			continue
		}
		conversation := change.Write.Conversation
		if conversation.Owner == nil {
			continue
		}
		key := strconv.FormatInt(int64(conversation.Owner.TaskID), 10)
		if node(key) != nil {
			created[key] = append(created[key], conversation.ID)
		}
	}
	for key, ids := range created {
		current := node(key)
		if current == nil {
			continue
		}
		merged := append(append([]Id{}, current.Conversations...), ids...)
		sort.Slice(merged, func(i, j int) bool { return merged[i] < merged[j] })
		ops = append(ops, delta.Op{Verb: delta.VerbSet, Path: delta.Path{"tasks", key, "conversations"}, Value: idsValue(merged)})
	}
	if len(ops) == 0 {
		return
	}
	// Apply the same changes to a fresh graph so observers never see a mutated
	// previous revision.
	next := TaskGraph{Tasks: make(map[string]TaskGraphNode, len(mount.value.Tasks))}
	for key, value := range mount.value.Tasks {
		next.Tasks[key] = value
	}
	for key, value := range changed {
		if value == nil {
			delete(next.Tasks, key)
		} else {
			next.Tasks[key] = *value
		}
	}
	for key, ids := range created {
		if current, present := next.Tasks[key]; present {
			merged := append(append([]Id{}, current.Conversations...), ids...)
			sort.Slice(merged, func(i, j int) bool { return merged[i] < merged[j] })
			current.Conversations = merged
			next.Tasks[key] = current
		}
	}
	mount.value = next
	frameContext := chord.Context(ctx)
	if frameContext != nil {
		frameContext = context.WithoutCancel(frameContext)
	}
	v.mu.Lock()
	observers := make([]any, 0, len(mount.observers))
	for observer := range mount.observers {
		observers = append(observers, observer)
	}
	v.mu.Unlock()
	for _, observer := range observers {
		advanceObserver(observer, next, ops, frameContext)
	}
}

func advanceObserver(observer any, value TaskGraph, ops []delta.Op, ctx chord.Context) {
	switch typed := observer.(type) {
	case *CommittedStateSource[TaskGraph]:
		typed.Advance(value, ops, ctx)
	case *CommittedWatch[TaskGraph]:
		typed.Advance(value, ops, ctx)
	}
}

func closeObserver(observer any) {
	switch typed := observer.(type) {
	case *CommittedStateSource[TaskGraph]:
		typed.CloseSession()
	case *CommittedWatch[TaskGraph]:
		typed.CloseSession()
	}
}

// taskGraphNode derives one graph node (upstream nodeOf).
func taskGraphNode(record TaskRecord, conversations []Id) TaskGraphNode {
	node := TaskGraphNode{
		ID: record.ID, Kind: record.Kind, ConversationID: record.ConversationID,
		Background: record.Background, AbortRequested: record.AbortRequested,
		State: taskGraphState(record), Conversations: conversations,
	}
	if node.Conversations == nil {
		node.Conversations = []Id{}
	}
	if record.Owner != nil {
		owner := *record.Owner
		node.Owner = &owner
	}
	return node
}

// taskGraphState derives the graph state (upstream stateOf).
func taskGraphState(record TaskRecord) TaskGraphState {
	state := record.State
	switch state.Status {
	case TaskPending, TaskRunning:
		return TaskGraphState{Status: state.Status, Phase: checkpointPhase(state.Checkpoint)}
	case TaskWaiting:
		on := append([]Id{}, state.On...)
		return TaskGraphState{Status: TaskWaiting, Phase: checkpointPhase(state.Checkpoint), On: on, Policy: state.Policy}
	default:
		outcome := ""
		if state.Outcome != nil {
			outcome = state.Outcome.Status
		}
		return TaskGraphState{Status: TaskCompleting, Outcome: outcome}
	}
}

func checkpointPhase(checkpoint json.RawMessage) string {
	if len(checkpoint) == 0 {
		return ""
	}
	var decoded map[string]any
	if err := json.Unmarshal(checkpoint, &decoded); err != nil {
		return ""
	}
	phase, _ := decoded["phase"].(string)
	return phase
}

// nodeValue renders one node as a JSON value for the op.
func nodeValue(node TaskGraphNode) chord.JsonValue {
	encoded, err := marshalJSONValue(node)
	if err != nil {
		return map[string]any{}
	}
	var value chord.JsonValue
	if err := unmarshalBytes([]byte(encoded), &value); err != nil {
		return map[string]any{}
	}
	return value
}

func idsValue(ids []Id) chord.JsonValue {
	encoded, err := marshalJSONValue(ids)
	if err != nil {
		return []any{}
	}
	var value chord.JsonValue
	if err := unmarshalBytes([]byte(encoded), &value); err != nil {
		return []any{}
	}
	return value
}

func nodesJSONEqual(left TaskGraphNode, right TaskGraphNode) bool {
	leftJSON, leftErr := marshalJSONValue(left)
	rightJSON, rightErr := marshalJSONValue(right)
	return leftErr == nil && rightErr == nil && leftJSON == rightJSON
}
