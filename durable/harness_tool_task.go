package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
)

// Port of the built-in tool task (harness/tool.ts).

func toolInputOf(raw json.RawMessage) ToolTaskInput {
	var input ToolTaskInput
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &input)
	}
	return input
}

func toolCheckpointOf(raw json.RawMessage) ToolTaskCheckpoint {
	var checkpoint ToolTaskCheckpoint
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &checkpoint)
	}
	return checkpoint
}

func toolArgsOf(raw json.RawMessage) JsonObject {
	args := JsonObject{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &args)
	}
	return args
}

// readToolCall is the tool call callId of the assistant entry.
func readToolCall(runtime TaskRuntime, input ToolTaskInput, ctx chord.Context) (ai.ToolCall, error) {
	entry, err := runtime.Entry(ctx, input.Assistant)
	if err != nil {
		return ai.ToolCall{}, err
	}
	if entry != nil && len(entry.Model) > 0 {
		if message, ok := entry.Model[0].(*ai.AssistantMessage); ok {
			for _, content := range message.Content {
				if call, ok := content.(ai.ToolCall); ok && call.ID == input.CallID {
					return call, nil
				}
			}
		}
	}
	return ai.ToolCall{}, fmt.Errorf("Entry %d has no tool call %s", input.Assistant, input.CallID)
}

func findTool(tools []ToolRegistration, name string) *ToolRegistration {
	for index := range tools {
		if tools[index].Name == name {
			return &tools[index]
		}
	}
	return nil
}

// toolEnding is how a tool task ends; the result entry is appended either way.
type toolEnding struct {
	Status  string
	Message string
}

var toolCompleted = toolEnding{Status: OutcomeCompleted}

// toolCallPhase resolves the called tool, validates the arguments, runs the
// beforeTool hooks, records intent, and executes.
func toolCallPhase(task RunningTask, runtime TaskRuntime, ctx chord.Context) error {
	input := toolInputOf(task.Input)
	call, err := readToolCall(runtime, input, ctx)
	if err != nil {
		return err
	}
	agent, err := runtime.Agent(ctx)
	if err != nil {
		return err
	}
	tool := findTool(agent.Tools, call.Name)
	if tool == nil {
		return settleTool(runtime, call, toolCompleted, func(map[string]any) ToolExecutionResult {
			return HarnessError("tool_unavailable", "Tool "+call.Name+" is not available")
		}, ctx)
	}
	args, errText := PrepareArguments(*tool, toolArgsOf(call.Arguments))
	if errText != nil {
		return settleTool(runtime, call, toolCompleted, func(map[string]any) ToolExecutionResult {
			return InvalidArguments(*errText)
		}, ctx)
	}
	if args, errText = ValidateArguments(*tool, call, args); errText != nil {
		return settleTool(runtime, call, toolCompleted, func(map[string]any) ToolExecutionResult {
			return InvalidArguments(*errText)
		}, ctx)
	}
	var block *string
	if err := runtime.Hooks().Each("beforeTool", func(handler any) error {
		if block != nil {
			return nil
		}
		hooks, ok := handler.(ToolHooks)
		if !ok || hooks.BeforeTool == nil {
			return nil
		}
		decision, err := hooks.BeforeTool(call, runtime, ctx)
		if err != nil {
			if runtime.Signal().Err() != nil {
				return err
			}
			text := err.Error()
			block = &text
			return nil
		}
		if decision != nil {
			if decision.Block != nil {
				block = decision.Block
			} else if decision.Arguments != nil {
				args = decision.Arguments
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if block != nil {
		text := *block
		return settleTool(runtime, call, toolCompleted, func(map[string]any) ToolExecutionResult {
			return HarnessError("blocked", "Tool call blocked: "+text)
		}, ctx)
	}
	final := args
	if final, errText = ValidateArguments(*tool, call, args); errText != nil {
		return settleTool(runtime, call, toolCompleted, func(map[string]any) ToolExecutionResult {
			return InvalidArguments(*errText)
		}, ctx)
	}
	if err := runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		live, err := tx.Doc(LiveDoc.Definition, runtime.ConversationID())
		if err != nil {
			return nil, err
		}
		if slot := ToolSlotOf(live, runtime.TaskID()); slot != nil {
			slot["status"] = ToolSlotRunning
		}
		replay := tool.Replay
		if replay == "" {
			replay = "unsafe"
		}
		encoded, err := marshalJSONValue(map[string]any{
			"phase": ToolPhaseExecute, "arguments": final, "replay": replay,
		})
		if err != nil {
			return nil, err
		}
		return &NextTaskState{Status: TaskRunning, Checkpoint: json.RawMessage(encoded)}, nil
	}, ctx); err != nil {
		return err
	}
	return runTool(runtime, call, *tool, final, ctx)
}

// toolExecutePhase is reached only by recovery and applies the replay rule.
func toolExecutePhase(task RunningTask, runtime TaskRuntime, ctx chord.Context) error {
	checkpoint := toolCheckpointOf(task.State.Checkpoint)
	input := toolInputOf(task.Input)
	call, err := readToolCall(runtime, input, ctx)
	if err != nil {
		return err
	}
	agent, err := runtime.Agent(ctx)
	if err != nil {
		return err
	}
	tool := findTool(agent.Tools, call.Name)
	if checkpoint.Replay == "safe" && tool != nil && tool.Replay == "safe" {
		if err := runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
			live, err := tx.Doc(LiveDoc.Definition, runtime.ConversationID())
			if err != nil {
				return nil, err
			}
			if slot := ToolSlotOf(live, runtime.TaskID()); slot != nil {
				ClearProgress(slot)
			}
			return nil, nil
		}, ctx); err != nil {
			return err
		}
		return runTool(runtime, call, *tool, checkpoint.Arguments, ctx)
	}
	message := fmt.Sprintf("Tool %s was interrupted and may have partially run", call.Name)
	return settleTool(runtime, call, toolEnding{Status: OutcomeFailed, Message: message}, func(slot map[string]any) ToolExecutionResult {
		return FromSlot(slot, "interrupted", message)
	}, ctx)
}

func toolAbortHandler(task RunningTask, runtime TaskRuntime, ctx chord.Context) error {
	call, err := readToolCall(runtime, toolInputOf(task.Input), ctx)
	if err != nil {
		return err
	}
	message := fmt.Sprintf("Tool %s was aborted", call.Name)
	return settleTool(runtime, call, toolEnding{Status: OutcomeAborted}, func(slot map[string]any) ToolExecutionResult {
		return FromSlot(slot, "aborted", message)
	}, ctx)
}

// settleTool commits the tool's terminal state: append its result entry, mark
// its slot done, and complete or end aborted/failed with the entry id.
func settleTool(runtime TaskRuntime, call ai.ToolCall, ending toolEnding, build func(slot map[string]any) ToolExecutionResult, ctx chord.Context) error {
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		live, err := tx.Doc(LiveDoc.Definition, runtime.ConversationID())
		if err != nil {
			return nil, err
		}
		slot := ToolSlotOf(live, runtime.TaskID())
		result := build(slot)
		entry, err := AppendToolResult(tx, runtime.ConversationID(), call, result, runtime.Now())
		if err != nil {
			return nil, err
		}
		if slot != nil {
			FinishSlot(slot, &entry.ID)
		}
		if ending.Status == OutcomeAborted {
			return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{
				Status: OutcomeAborted, Result: dataJSON(map[string]any{"entryId": entry.ID}),
			}}, nil
		}
		if ending.Status == OutcomeFailed {
			return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{
				Status: OutcomeFailed, Error: &StoredError{Message: ending.Message},
				Result: dataJSON(map[string]any{"entryId": entry.ID}),
			}}, nil
		}
		payload := map[string]any{"entryId": entry.ID}
		if result.Control != nil {
			payload["control"] = result.Control
		}
		return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{
			Status: OutcomeCompleted, Result: dataJSON(payload),
		}}, nil
	}, ctx)
}

// reportedTool is what a running tool reported through its api.
type reportedTool struct {
	output      *OutputBuffer
	limits      OutputLimits
	diagnostics []ToolDiagnostic
	details     chord.JsonValue
}

// toolExecutionApi is the operation set available to one tool invocation.
type toolExecutionApi struct {
	runtime      TaskRuntime
	call         ai.ToolCall
	reported     *reportedTool
	progress     *Progress
	env          ExecutionEnv
	outputWindow *ShellOutputWindow
	ended        bool
}

func (a *toolExecutionApi) assertLive() error {
	if a.ended {
		return fmt.Errorf("Tool call %s has settled", a.call.ID)
	}
	return nil
}

func (a *toolExecutionApi) TaskID() Id         { return a.runtime.TaskID() }
func (a *toolExecutionApi) ConversationID() Id { return a.runtime.ConversationID() }
func (a *toolExecutionApi) CallID() string     { return a.call.ID }
func (a *toolExecutionApi) Registry() RegistrySnapshot {
	return a.runtime.Registry()
}
func (a *toolExecutionApi) Agent(ctx chord.Context) (Agent, error) { return a.runtime.Agent(ctx) }
func (a *toolExecutionApi) Models() *ai.Models                     { return a.runtime.Models() }
func (a *toolExecutionApi) Env() ExecutionEnv                      { return a.env }
func (a *toolExecutionApi) OutputWindow() *ShellOutputWindow       { return a.outputWindow }

func (a *toolExecutionApi) Output(chunk []byte, skipped ...*ShellOutputSkip) {
	if a.assertLive() != nil {
		return
	}
	var omitted *ShellOutputSkip
	if len(skipped) > 0 {
		omitted = skipped[0]
	}
	if a.reported.output.Push(string(chunk), omitted) {
		a.progress.Mark()
	}
}

func (a *toolExecutionApi) Diagnostic(diagnostic ToolDiagnostic) {
	if a.assertLive() != nil {
		return
	}
	a.reported.diagnostics = append(a.reported.diagnostics, diagnostic)
	a.progress.Mark()
}

func (a *toolExecutionApi) Details(value chord.JsonValue, ctx chord.Context) error {
	if err := a.assertLive(); err != nil {
		return err
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	a.reported.details = value
	waiter := a.progress.MarkAndWait()
	if waiter != nil {
		return waiter.Wait()
	}
	return nil
}

func (a *toolExecutionApi) Commit(change func(tx *Transaction) (any, error), ctx chord.Context) (any, error) {
	var result any
	err := a.runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		value, err := change(tx)
		result = value
		return nil, err
	}, ctx)
	return result, err
}

func (a *toolExecutionApi) Memo(ctx chord.Context, name string) (chord.JsonValue, bool, error) {
	return a.runtime.Memo(ctx, name)
}

func (a *toolExecutionApi) MemoSet(ctx chord.Context, name string, candidate chord.JsonValue) (chord.JsonValue, error) {
	return a.runtime.MemoSet(ctx, name, candidate)
}

func (a *toolExecutionApi) CreateTask(task Task, input chord.JsonValue, options TaskOptions, ctx chord.Context) (Id, error) {
	var id Id
	_, err := a.Commit(func(tx *Transaction) (any, error) {
		encoded, err := marshalJSONValue(input)
		if err != nil {
			return nil, err
		}
		created, err := tx.CreateTask(task.Definition, json.RawMessage(encoded), options)
		id = created
		return created, err
	}, ctx)
	return id, err
}

func (a *toolExecutionApi) GetTask(id Id, ctx chord.Context) (*TaskRecord, error) {
	return a.runtime.GetTask(id, ctx)
}

func (a *toolExecutionApi) WaitForTask(id Id, ctx chord.Context) (SettledTask, error) {
	return a.runtime.WaitForTask(id, ctx)
}

func (a *toolExecutionApi) Conversation(id Id, ctx chord.Context) (ConversationHandle, bool, error) {
	return a.runtime.Conversation(id, ctx)
}

func (a *toolExecutionApi) Snapshot(ctx chord.Context, definition DocDefinition, args ...any) (chord.JsonValue, bool, error) {
	return a.runtime.Snapshot(ctx, definition, args...)
}

func (a *toolExecutionApi) SnapshotAsOf(ctx chord.Context, definition DocDefinition, conversationID Id, at Id) (chord.JsonValue, bool, error) {
	return a.runtime.SnapshotAsOf(ctx, definition, conversationID, at)
}

func (a *toolExecutionApi) WatchDoc(ctx chord.Context, definition DocDefinition, args ...any) (DocumentWatch[chord.JsonValue], bool, error) {
	return a.runtime.WatchDoc(ctx, definition, args...)
}

// runTool executes with the resolved implementation, then settles its result.
func runTool(runtime TaskRuntime, call ai.ToolCall, tool ToolRegistration, args JsonObject, ctx chord.Context) error {
	limits := OutputLimits{MaxBytes: DefaultMaxBytes, MaxLines: DefaultMaxLines, Retain: RetainHead}
	if tool.OutputLimits != nil {
		if tool.OutputLimits.MaxBytes > 0 {
			limits.MaxBytes = tool.OutputLimits.MaxBytes
		}
		if tool.OutputLimits.MaxLines > 0 {
			limits.MaxLines = tool.OutputLimits.MaxLines
		}
		if tool.OutputLimits.Retain != "" {
			limits.Retain = tool.OutputLimits.Retain
		}
	}
	reported := &reportedTool{output: NewOutputBuffer(limits), limits: limits}
	progressSettings := runtime.Settings().Progress
	progress := publishToolProgress(runtime, reported, ctx, progressSettings.OutputIntervalMs)
	var outputWindow *ShellOutputWindow
	if limits.Retain == RetainTail {
		outputWindow = &ShellOutputWindow{
			MaxBytes: limits.MaxBytes, MaxLines: limits.MaxLines,
			MinIntervalMs: progressSettings.OutputIntervalMs, BytesPerSecond: defaultProgressBytesPerSecond,
		}
	}
	api := &toolExecutionApi{
		runtime: runtime, call: call, reported: reported, progress: progress, outputWindow: outputWindow,
	}
	var result ToolExecutionResult
	ending := toolCompleted
	env, envErr := runtime.Env(ctx)
	if envErr != nil {
		isError := true
		result = ToolExecutionResult{IsError: &isError, Diagnostics: []ToolDiagnostic{ToolDiagnosticOf("tool_error", envErr.Error())}}
		ending = toolEnding{Status: OutcomeFailed, Message: fmt.Sprintf("Tool %s threw", call.Name)}
	} else if tool.Execute == nil {
		isError := true
		result = ToolExecutionResult{IsError: &isError, Diagnostics: []ToolDiagnostic{ToolDiagnosticOf("tool_error", "Tool has no implementation")}}
		ending = toolEnding{Status: OutcomeFailed, Message: fmt.Sprintf("Tool %s threw", call.Name)}
	} else {
		api.env = env
		var err error
		result, err = tool.Execute(args, api, ctx)
		if err != nil {
			if runtime.Signal().Err() != nil {
				api.ended = true
				for _, waiter := range progress.Stop() {
					waiter.Reject(err)
				}
				return err
			}
			isError := true
			result = ToolExecutionResult{IsError: &isError, Diagnostics: []ToolDiagnostic{ToolDiagnosticOf("tool_error", err.Error())}}
			ending = toolEnding{Status: OutcomeFailed, Message: fmt.Sprintf("Tool %s threw", call.Name)}
		}
	}
	api.ended = true
	reported.output.End()
	pending := progress.Stop()
	final, err := finalToolResult(runtime, call, result, reported, ctx)
	if err != nil {
		for _, waiter := range pending {
			waiter.Reject(err)
		}
		return err
	}
	if err := settleTool(runtime, call, ending, func(map[string]any) ToolExecutionResult { return final }, ctx); err != nil {
		for _, waiter := range pending {
			waiter.Reject(err)
		}
		return err
	}
	for _, waiter := range pending {
		waiter.Resolve()
	}
	return nil
}

// writtenTool is what a progress commit last wrote.
type writtenTool struct {
	text        string
	details     chord.JsonValue
	diagnostics int
}

// publishToolProgress commits what the tool reported into its tool slot.
func publishToolProgress(runtime TaskRuntime, reported *reportedTool, ctx chord.Context, intervalMs int) *Progress {
	written := &writtenTool{}
	return NewProgress(func() (int, error) {
		snapshot := reported.output.Snapshot()
		current := writtenTool{text: snapshot.Text, details: reported.details, diagnostics: len(reported.diagnostics)}
		added := reported.diagnostics[written.diagnostics:current.diagnostics]
		detailsChanged := !sameJSONValue(current.details, written.details)
		bytes := 0
		if snapshot.Text != written.text {
			shared := 0
			if strings.HasPrefix(snapshot.Text, written.text) {
				shared = len(written.text)
			} else {
				shared = delta.Overlap(written.text, snapshot.Text, 65_536, 0, 0)
			}
			bytes += UTF8ByteLength(snapshot.Text[shared:])
		}
		if detailsChanged {
			bytes += UTF8ByteLength(string(dataJSON(current.details)))
		}
		if len(added) > 0 {
			bytes += UTF8ByteLength(string(dataJSON(added)))
		}
		err := runtime.Commit(func(tx *Transaction, currentTask RunningTask) (*NextTaskState, error) {
			live, err := tx.Doc(LiveDoc.Definition, runtime.ConversationID())
			if err != nil {
				return nil, err
			}
			slot := ToolSlotOf(live, runtime.TaskID())
			if slot == nil {
				return nil, nil
			}
			if output, _ := slot["output"].(string); output != snapshot.Text {
				slot["output"] = snapshot.Text
			}
			if snapshot.DroppedBytes > 0 {
				slot["droppedBytes"] = snapshot.DroppedBytes
			}
			if snapshot.DroppedLines > 0 {
				slot["droppedLines"] = snapshot.DroppedLines
			}
			if detailsChanged && current.details != nil {
				AssignJSON(slot, "details", current.details)
			}
			if len(added) > 0 {
				list, _ := slot["diagnostics"].([]any)
				for _, diagnostic := range added {
					value, err := jsonValueOf(diagnostic)
					if err != nil {
						continue
					}
					list = append(list, value)
				}
				slot["diagnostics"] = list
			}
			return nil, nil
		}, ctx)
		*written = current
		return bytes, err
	}, func(err error) {
		if runtime.Signal().Err() == nil {
			runtime.Report(err)
		}
	}, intervalMs)
}

// finalToolResult is the settled result: retained output and last details as
// fallbacks, diagnostics after those reported, afterTool applied, and explicit
// text bounded with the truncation diagnostic last.
func finalToolResult(runtime TaskRuntime, call ai.ToolCall, result ToolExecutionResult, reported *reportedTool, ctx chord.Context) (ToolExecutionResult, error) {
	harness := []ToolDiagnostic{}
	var retained *BoundedOutput
	content := result.Content
	if result.Content == nil {
		snapshot := reported.output.Snapshot()
		retained = &snapshot
		if snapshot.Text == "" {
			content = []ai.UserContent{}
		} else {
			content = []ai.UserContent{ai.TextContent{Text: snapshot.Text}}
		}
	}
	final := ToolExecutionResult{
		Content: content, IsError: result.IsError, Details: result.Details, Usage: result.Usage, Control: result.Control,
		Diagnostics: append(append([]ToolDiagnostic{}, reported.diagnostics...), result.Diagnostics...),
	}
	if final.Details == nil {
		final.Details = reported.details
	}
	if err := runtime.Hooks().Each("afterTool", func(handler any) error {
		hooks, ok := handler.(ToolHooks)
		if !ok || hooks.AfterTool == nil {
			return nil
		}
		replaced, err := hooks.AfterTool(call, final, runtime, ctx)
		if err != nil {
			return err
		}
		if replaced != nil {
			final = *replaced
		}
		return nil
	}); err != nil {
		return ToolExecutionResult{}, err
	}
	// The retained output's truncation applies only while afterTool kept that
	// content.
	if retained != nil && retained.DroppedBytes > 0 && sameContent(final.Content, content) {
		harness = append(harness, Truncated(retained.DroppedBytes, retained.DroppedLines, &reported.limits.Retain))
	}
	bounded, droppedBytes, droppedLines := BoundContent(final.Content, reported.limits)
	if droppedBytes > 0 {
		harness = append(harness, Truncated(droppedBytes, droppedLines, &reported.limits.Retain))
	}
	final.Content = bounded
	final.Diagnostics = append(final.Diagnostics, harness...)
	return final, nil
}

func sameContent(left, right []ai.UserContent) bool {
	if len(left) != len(right) {
		return false
	}
	if len(left) == 0 {
		return true
	}
	return &left[0] == &right[0]
}

var _ ToolExecutionApi = (*toolExecutionApi)(nil)
var _ = context.Background
