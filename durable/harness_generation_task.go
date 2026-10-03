package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of the built-in generation task (harness/generation.ts).

const defaultPollAfterMs = 5000

func generationCheckpointOf(raw json.RawMessage) GenerationCheckpoint {
	var checkpoint GenerationCheckpoint
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &checkpoint)
	}
	return checkpoint
}

func shownMap(shown *orderedMap[string]) map[string]string {
	values := map[string]string{}
	for _, key := range shown.Keys() {
		value, _ := shown.Get(key)
		values[key] = value
	}
	return values
}

// readGenerationCalls is the calls callIDs of the assistant entry, in order.
func readGenerationCalls(runtime TaskRuntime, assistant Id, callIDs []string, ctx chord.Context) ([]ai.ToolCall, error) {
	entry, err := runtime.Entry(ctx, assistant)
	if err != nil {
		return nil, err
	}
	calls := []ai.ToolCall{}
	if entry != nil && len(entry.Model) > 0 {
		if message, ok := entry.Model[0].(*ai.AssistantMessage); ok {
			for _, content := range message.Content {
				if call, ok := content.(ai.ToolCall); ok {
					calls = append(calls, call)
				}
			}
		}
	}
	out := []ai.ToolCall{}
	for _, id := range callIDs {
		for _, call := range calls {
			if call.ID == id {
				out = append(out, call)
				break
			}
		}
	}
	return out, nil
}

// appendAssistant appends a provider result and adds its usage in the same
// commit.
func appendAssistant(tx *Transaction, conversationID Id, message *ai.AssistantMessage) (*EntryRecord, error) {
	if err := RecordUsage(context.Background(), tx, conversationID, UsageBucketModels,
		message.Provider+"/"+message.Model, message.Usage); err != nil {
		return nil, err
	}
	return tx.AppendEntry(conversationID, EntryDraft{Kind: AssistantEntry.Kind, Model: []ai.Message{message}})
}

// failModelError settles the run's inputs unanswered with model_error and
// fails with text.
func failGenerationModelError(runtime TaskRuntime, text string, ctx chord.Context) error {
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		live, err := tx.Doc(LiveDoc.Definition, runtime.ConversationID())
		if err != nil {
			return nil, err
		}
		reason := "model_error"
		if err := EndRun(tx, live, runtime.TaskID(), SubmissionSettlement{Status: SubmissionUnanswered, Reason: &reason, Detail: dataJSON(text)}); err != nil {
			return nil, err
		}
		return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{
			Status: OutcomeFailed,
			Error:  &StoredError{Message: text, Detail: dataJSON(map[string]any{"reason": "model_error"})},
		}}, nil
	}, ctx)
}

// failGenerationNoModel settles the run's inputs unanswered with no_model.
func failGenerationNoModel(runtime TaskRuntime, ref *ModelRef, ctx chord.Context) error {
	message := "No model is configured"
	if ref != nil {
		message = fmt.Sprintf("Model %s/%s is not available", ref.Provider, ref.ModelID)
	}
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		live, err := tx.Doc(LiveDoc.Definition, runtime.ConversationID())
		if err != nil {
			return nil, err
		}
		reason := "no_model"
		if err := EndRun(tx, live, runtime.TaskID(), SubmissionSettlement{Status: SubmissionUnanswered, Reason: &reason}); err != nil {
			return nil, err
		}
		return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{
			Status: OutcomeFailed,
			Error:  &StoredError{Message: message, Detail: dataJSON(map[string]any{"reason": "no_model"})},
		}}, nil
	}, ctx)
}

// toolDeclarations converts registrations to their transcript declarations.
func toolDeclarations(tools []ToolRegistration) []ai.Tool {
	declarations := make([]ai.Tool, 0, len(tools))
	for _, tool := range tools {
		declarations = append(declarations, ai.ToToolDeclaration(tool.Tool))
	}
	return declarations
}

func generationModel(runtime TaskRuntime, ref *ModelRef) *ai.Model {
	if ref == nil || runtime.Models() == nil {
		return nil
	}
	return runtime.Models().GetModel(ref.Provider, ref.ModelID)
}

// generationPrepare renders the system prompt and the planned entries, starts a
// blocking compaction when the context is too large, and moves to request.
func generationPrepare(task RunningTask, runtime TaskRuntime, ctx chord.Context) error {
	conversationID := runtime.ConversationID()
	agent, err := runtime.Agent(ctx)
	if err != nil {
		return err
	}
	settings := runtime.Settings()
	ref := agent.Model
	model := generationModel(runtime, ref)
	checkpoint := generationCheckpointOf(task.State.Checkpoint)
	if model == nil {
		return failGenerationNoModel(runtime, ref, ctx)
	}
	if checkpoint.Compacted != nil && checkpoint.Overflow != nil {
		outcomes, err := runtime.Outcomes([]Id{*checkpoint.Compacted}, ctx)
		if err != nil {
			return err
		}
		if len(outcomes) == 0 || outcomes[0].Status != OutcomeCompleted || len(outcomes[0].Result) == 0 {
			return failGenerationModelError(runtime, *checkpoint.Overflow, ctx)
		}
		var result CompactionResult
		_ = json.Unmarshal(outcomes[0].Result, &result)
		if result.EntryID == nil {
			return failGenerationModelError(runtime, *checkpoint.Overflow, ctx)
		}
	}
	view, err := runtime.Context(conversationID, ctx, nil)
	if err != nil {
		return err
	}
	shown := ReplaySections(view.Messages)
	report := func(err error) { runtime.Report(err) }
	env, envErr := runtime.Env(ctx)
	if envErr != nil {
		if runtime.Signal().Err() != nil {
			return envErr
		}
		report(envErr)
	}
	input := PromptInput{ConversationID: conversationID, Agent: agent, Env: env, Shown: shownMap(shown), Read: runtime}
	desired, err := RenderSections(agent.Sections, input, shown, report, ctx)
	if err != nil {
		return err
	}
	entries := PlanSystemEntries(view, desired, toolDeclarations(agent.Tools), runtime.Now())
	threshold := ""
	if checkpoint.Compacted == nil {
		threshold = ThresholdCompaction(view, entries, int(model.ContextWindow), settings.Compaction)
	}
	if threshold == ThresholdBlocking {
		return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
			child, err := CreateCompaction(tx, conversationID, CompactionInput{Reason: CompactionThreshold}, &current.ID)
			if err != nil {
				return nil, err
			}
			encoded, err := marshalJSONValue(map[string]any{
				"phase": GenerationPhasePrepare, "attempt": checkpoint.Attempt, "compacted": child,
			})
			if err != nil {
				return nil, err
			}
			return &NextTaskState{Status: TaskWaiting, Checkpoint: json.RawMessage(encoded), On: []Id{child}, Policy: JoinAllSettled}, nil
		}, ctx)
	}
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		page, err := tx.ScanEntries(EntryQuery{ConversationID: conversationID}, 1, nil)
		if err != nil {
			return nil, err
		}
		var cutoff *Id
		if len(page.Items) > 0 {
			id := page.Items[0].ID
			cutoff = &id
		}
		for _, entry := range entries {
			appended, err := tx.AppendEntry(conversationID, EntryDraft{Kind: SystemEntry.Kind, Model: entry.Model, Data: entry.Data, Head: entry.Head, Edits: entry.Edits})
			if err != nil {
				return nil, err
			}
			id := appended.ID
			cutoff = &id
		}
		if cutoff == nil {
			return nil, fmt.Errorf("Conversation %d has no entries to send", conversationID)
		}
		if threshold == ThresholdBackground {
			live, err := tx.Doc(LiveDoc.Definition, conversationID)
			if err != nil {
				return nil, err
			}
			if object, ok := live.(map[string]any); ok && object["compactions"] == nil {
				if _, err := CreateCompaction(tx, conversationID, CompactionInput{Reason: CompactionThreshold}, nil); err != nil {
					return nil, err
				}
			}
		}
		payload := map[string]any{
			"phase": GenerationPhaseRequest, "attempt": checkpoint.Attempt, "model": ref,
			"thinkingLevel": agent.ThinkingLevel, "streamOptions": settings.Stream, "cutoff": *cutoff,
		}
		if checkpoint.Compacted != nil {
			payload["compacted"] = *checkpoint.Compacted
		}
		encoded, err := marshalJSONValue(payload)
		if err != nil {
			return nil, err
		}
		return &NextTaskState{Status: TaskRunning, Checkpoint: json.RawMessage(encoded)}, nil
	}, ctx)
}

// generationRequest streams the request and classifies the response.
func generationRequest(task RunningTask, runtime TaskRuntime, ctx chord.Context) error {
	checkpoint := generationCheckpointOf(task.State.Checkpoint)
	conversationID := runtime.ConversationID()
	if err := runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		live, err := tx.Doc(LiveDoc.Definition, conversationID)
		if err != nil {
			return nil, err
		}
		if err := ConvertPartial(tx, live, conversationID); err != nil {
			return nil, err
		}
		if object, ok := live.(map[string]any); ok {
			object["generation"] = map[string]any{"attempt": checkpoint.Attempt}
		}
		return nil, nil
	}, ctx); err != nil {
		return err
	}
	model := generationModel(runtime, checkpoint.Model)
	if model == nil {
		return failGenerationNoModel(runtime, checkpoint.Model, ctx)
	}
	view, err := runtime.Context(conversationID, ctx, checkpoint.Cutoff)
	if err != nil {
		return err
	}
	messages := view.Messages
	if err := runtime.Hooks().Each("beforeRequest", func(handler any) error {
		hooks, ok := handler.(GenerationHooks)
		if !ok || hooks.BeforeRequest == nil {
			return nil
		}
		replaced, err := hooks.BeforeRequest(GenerationRequest{Messages: messages}, runtime, ctx)
		if err != nil {
			return err
		}
		if replaced != nil {
			messages = replaced.Messages
		}
		return nil
	}); err != nil {
		return err
	}
	options := generationStreamOptions(checkpoint.StreamOptions, checkpoint.ThinkingLevel, runtime.Signal())
	message, err := streamGenerationResponse(runtime, model, messages, options, checkpoint.Attempt, ctx)
	if err != nil {
		return err
	}
	return classifyGeneration(runtime, generationRequestInfo{
		Attempt: checkpoint.Attempt, Compacted: checkpoint.Compacted, Model: checkpoint.Model,
		Cutoff: checkpoint.Cutoff, Messages: view.Messages,
	}, message, ctx)
}

// generationRetry sleeps the durable backoff and returns to prepare.
func generationRetry(task RunningTask, runtime TaskRuntime, ctx chord.Context) error {
	checkpoint := generationCheckpointOf(task.State.Checkpoint)
	until := int64(0)
	if checkpoint.Until != nil {
		until = *checkpoint.Until
	}
	if err := runtime.Sleep(until, ctx); err != nil {
		return err
	}
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		live, err := tx.Doc(LiveDoc.Definition, runtime.ConversationID())
		if err != nil {
			return nil, err
		}
		if object, ok := live.(map[string]any); ok {
			object["generation"] = map[string]any{"attempt": checkpoint.Attempt + 1}
		}
		payload := map[string]any{"phase": GenerationPhasePrepare, "attempt": checkpoint.Attempt + 1}
		if checkpoint.Compacted != nil {
			payload["compacted"] = *checkpoint.Compacted
		}
		encoded, err := marshalJSONValue(payload)
		if err != nil {
			return nil, err
		}
		return &NextTaskState{Status: TaskRunning, Checkpoint: json.RawMessage(encoded)}, nil
	}, ctx)
}

// generationPoll waits for a deferred response and classifies it.
func generationPoll(task RunningTask, runtime TaskRuntime, ctx chord.Context) error {
	checkpoint := generationCheckpointOf(task.State.Checkpoint)
	model := generationModel(runtime, checkpoint.Model)
	if model == nil {
		return failGenerationNoModel(runtime, checkpoint.Model, ctx)
	}
	pollAt := int64(0)
	if checkpoint.PollAt != nil {
		pollAt = *checkpoint.PollAt
	}
	if err := runtime.Sleep(pollAt, ctx); err != nil {
		return err
	}
	if runtime.Models() == nil {
		return failGenerationNoModel(runtime, checkpoint.Model, ctx)
	}
	message, err := runtime.Models().FetchDeferred(model, checkpoint.Handle, &ai.ModelsStreamOptions{StreamOptions: ai.StreamOptions{Ctx: runtime.Signal()}})
	if err != nil {
		return err
	}
	return classifyGeneration(runtime, generationRequestInfo{
		Attempt: checkpoint.Attempt, Compacted: checkpoint.Compacted, Model: checkpoint.Model,
		Cutoff: checkpoint.Cutoff, PollAt: checkpoint.PollAt,
	}, message, ctx)
}

// generationTools starts the next pending call of a sequential round, or
// finishes the round.
func generationTools(task RunningTask, runtime TaskRuntime, ctx chord.Context) error {
	checkpoint := generationCheckpointOf(task.State.Checkpoint)
	if len(checkpoint.Pending) == 0 {
		return finishToolRound(runtime, checkpoint.Assistant, checkpoint.Tools, ctx)
	}
	next := checkpoint.Pending[0]
	rest := append([]string{}, checkpoint.Pending[1:]...)
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		live, err := tx.Doc(LiveDoc.Definition, runtime.ConversationID())
		if err != nil {
			return nil, err
		}
		taskID, err := CreateToolTask(tx, current.ID, checkpoint.Assistant, next)
		if err != nil {
			return nil, err
		}
		if object, ok := live.(map[string]any); ok {
			if tools, ok := object["tools"].([]any); ok {
				for _, item := range tools {
					slot, ok := item.(map[string]any)
					if ok && slot["callId"] == next && slot["taskId"] == nil {
						slot["taskId"] = taskID
					}
				}
			}
		}
		payload := map[string]any{
			"phase": GenerationPhaseTools, "assistant": checkpoint.Assistant,
			"tools": append(append([]Id{}, checkpoint.Tools...), taskID), "pending": rest,
		}
		encoded, err := marshalJSONValue(payload)
		if err != nil {
			return nil, err
		}
		return &NextTaskState{Status: TaskWaiting, Checkpoint: json.RawMessage(encoded), On: []Id{taskID}, Policy: JoinAllSettled}, nil
	}, ctx)
}

// generationAbort cancels a deferred poll, appends aborted results for the
// calls never started, and ends the run.
func generationAbort(task RunningTask, runtime TaskRuntime, ctx chord.Context) error {
	checkpoint := generationCheckpointOf(task.State.Checkpoint)
	if checkpoint.Phase == GenerationPhasePoll {
		if model := generationModel(runtime, checkpoint.Model); model != nil && runtime.Models() != nil {
			if err := runtime.Models().CancelDeferred(model, checkpoint.Handle, &ai.ModelsStreamOptions{StreamOptions: ai.StreamOptions{Ctx: runtime.Signal()}}); err != nil {
				runtime.Report(err)
			}
		}
	}
	conversationID := runtime.ConversationID()
	var unstarted []ai.ToolCall
	if checkpoint.Phase == GenerationPhaseTools {
		var err error
		unstarted, err = readGenerationCalls(runtime, checkpoint.Assistant, checkpoint.Pending, ctx)
		if err != nil {
			return err
		}
	}
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		live, err := tx.Doc(LiveDoc.Definition, conversationID)
		if err != nil {
			return nil, err
		}
		if err := ConvertPartial(tx, live, conversationID); err != nil {
			return nil, err
		}
		for _, call := range unstarted {
			if _, err := AppendToolResult(tx, conversationID, call, HarnessError("aborted", "Tool "+call.Name+" was aborted"), runtime.Now()); err != nil {
				return nil, err
			}
		}
		reason := "aborted"
		if err := EndRun(tx, live, runtime.TaskID(), SubmissionSettlement{Status: SubmissionUnanswered, Reason: &reason}); err != nil {
			return nil, err
		}
		return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeAborted}}, nil
	}, ctx)
}

// generationRequestInfo is what classification needs from the request.
type generationRequestInfo struct {
	Attempt   int
	Compacted *Id
	Model     *ModelRef
	Cutoff    *Id
	Messages  []ai.Message
	PollAt    *int64
}

// classifyGeneration classifies a terminal provider message in one commit.
func classifyGeneration(runtime TaskRuntime, request generationRequestInfo, message *ai.AssistantMessage, ctx chord.Context) error {
	if err := runtime.Signal().Err(); err != nil {
		return err
	}
	conversationID := runtime.ConversationID()
	if message.StopReason == ai.StopDeferred && message.Deferred != nil {
		pollAt := runtime.Now() + defaultPollAfterMs
		if message.Deferred.PollAfterMs != nil {
			pollAt = runtime.Now() + *message.Deferred.PollAfterMs
		}
		if request.PollAt != nil && *request.PollAt+1 > pollAt {
			pollAt = *request.PollAt + 1
		}
		return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
			live, err := tx.Doc(LiveDoc.Definition, conversationID)
			if err != nil {
				return nil, err
			}
			if object, ok := live.(map[string]any); ok {
				object["generation"] = map[string]any{"attempt": request.Attempt, "deferred": map[string]any{"pollAt": pollAt}}
			}
			payload := map[string]any{
				"phase": GenerationPhasePoll, "attempt": request.Attempt, "model": request.Model,
				"cutoff": request.Cutoff, "handle": message.Deferred, "pollAt": pollAt,
			}
			if request.Compacted != nil {
				payload["compacted"] = *request.Compacted
			}
			encoded, err := marshalJSONValue(payload)
			if err != nil {
				return nil, err
			}
			return &NextTaskState{Status: TaskRunning, Checkpoint: json.RawMessage(encoded)}, nil
		}, ctx)
	}
	if err := runtime.Hooks().Each("afterResponse", func(handler any) error {
		hooks, ok := handler.(GenerationHooks)
		if !ok || hooks.AfterResponse == nil {
			return nil
		}
		return hooks.AfterResponse(message, runtime, ctx)
	}); err != nil {
		return err
	}
	calls := []ai.ToolCall{}
	for _, content := range message.Content {
		if call, ok := content.(ai.ToolCall); ok {
			calls = append(calls, call)
		}
	}
	if message.StopReason == ai.StopToolUse && len(calls) > 0 {
		return startToolRound(runtime, request, message, calls, ctx)
	}
	if message.StopReason == ai.StopStop || message.StopReason == ai.StopLength || message.StopReason == ai.StopToolUse {
		return answerGeneration(runtime, message, ctx)
	}
	settings := runtime.Settings()
	overflow := message.StopReason == ai.StopError && modelOverflow(runtime, request, message)
	if overflow && request.Compacted == nil && settings.Compaction.Enabled {
		policy := settings.Compaction
		view, err := runtime.Context(conversationID, ctx, request.Cutoff)
		if err != nil {
			return err
		}
		if SelectCut(view, policy.KeepRecentTokens) != nil {
			text := "Context overflow"
			if message.ErrorMessage != nil {
				text = *message.ErrorMessage
			}
			return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
				live, err := tx.Doc(LiveDoc.Definition, conversationID)
				if err != nil {
					return nil, err
				}
				if _, err := appendAssistant(tx, conversationID, message); err != nil {
					return nil, err
				}
				if object, ok := live.(map[string]any); ok {
					delete(object, "generation")
				}
				child, err := CreateCompaction(tx, conversationID, CompactionInput{Reason: CompactionOverflow}, &current.ID)
				if err != nil {
					return nil, err
				}
				encoded, err := marshalJSONValue(map[string]any{
					"phase": GenerationPhasePrepare, "attempt": request.Attempt, "compacted": child, "overflow": text,
				})
				if err != nil {
					return nil, err
				}
				return &NextTaskState{Status: TaskWaiting, Checkpoint: json.RawMessage(encoded), On: []Id{child}, Policy: JoinAllSettled}, nil
			}, ctx)
		}
	}
	policy := settings.Retry
	retry := message.StopReason == ai.StopError && !overflow && ai.IsRetryableAssistantError(message) &&
		policy.Enabled && request.Attempt <= policy.MaxRetries
	until := int64(0)
	if retry {
		until = runtime.Now() + ai.RetryDelayMS(retryPolicyOf(policy), request.Attempt)
	}
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		live, err := tx.Doc(LiveDoc.Definition, conversationID)
		if err != nil {
			return nil, err
		}
		if _, err := appendAssistant(tx, conversationID, message); err != nil {
			return nil, err
		}
		if retry {
			if object, ok := live.(map[string]any); ok {
				object["generation"] = map[string]any{"attempt": request.Attempt, "retry": map[string]any{"at": until, "error": errorMessageOf(message)}}
			}
			payload := map[string]any{"phase": GenerationPhaseRetry, "attempt": request.Attempt, "until": until}
			if request.Compacted != nil {
				payload["compacted"] = *request.Compacted
			}
			encoded, err := marshalJSONValue(payload)
			if err != nil {
				return nil, err
			}
			return &NextTaskState{Status: TaskRunning, Checkpoint: json.RawMessage(encoded)}, nil
		}
		text := fmt.Sprintf("Model response ended with stop reason %s", message.StopReason)
		if message.ErrorMessage != nil {
			text = *message.ErrorMessage
		}
		reason := "model_error"
		if err := EndRun(tx, live, runtime.TaskID(), SubmissionSettlement{Status: SubmissionUnanswered, Reason: &reason, Detail: dataJSON(text)}); err != nil {
			return nil, err
		}
		return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{
			Status: OutcomeFailed, Error: &StoredError{Message: text, Detail: dataJSON(map[string]any{"reason": "model_error"})},
		}}, nil
	}, ctx)
}

func modelOverflow(runtime TaskRuntime, request generationRequestInfo, message *ai.AssistantMessage) bool {
	model := generationModel(runtime, request.Model)
	if model == nil {
		return false
	}
	return ai.IsContextOverflow(message, model.ContextWindow)
}

// answerGeneration is a final answer; the final boundary places queued items.
func answerGeneration(runtime TaskRuntime, message *ai.AssistantMessage, ctx chord.Context) error {
	var continuation *UserInput
	if err := runtime.Hooks().Each("onYield", func(handler any) error {
		if continuation != nil {
			return nil
		}
		hooks, ok := handler.(GenerationHooks)
		if !ok || hooks.OnYield == nil {
			return nil
		}
		decision, err := hooks.OnYield(message, runtime, ctx)
		if err != nil {
			return err
		}
		if decision != nil {
			continuation = decision.Continue
		}
		return nil
	}); err != nil {
		return err
	}
	conversationID := runtime.ConversationID()
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		settings := runtime.Settings()
		boundary, err := PrepareBoundary(tx, conversationID, QueueModes{SteeringMode: settings.SteeringMode, FollowUpMode: settings.FollowUpMode})
		if err != nil {
			return nil, err
		}
		live, err := tx.Doc(LiveDoc.Definition, conversationID)
		if err != nil {
			return nil, err
		}
		entry, err := appendAssistant(tx, conversationID, message)
		if err != nil {
			return nil, err
		}
		result := &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{
			Status: OutcomeCompleted, Result: dataJSON(map[string]any{"entryId": entry.ID}),
		}}
		applied, err := ApplyBoundary(tx, boundary, "final", runtime.Now())
		if err != nil {
			return nil, err
		}
		if continuation != nil && len(applied.Users) == 0 && !applied.Reset {
			userMessage, err := userMessageFromContent(*continuation, runtime.Now())
			if err != nil {
				return nil, err
			}
			if _, err := tx.AppendEntry(conversationID, EntryDraft{Kind: UserEntry.Kind, Model: []ai.Message{userMessage}}); err != nil {
				return nil, err
			}
			next, err := CreateGeneration(tx, conversationID)
			if err != nil {
				return nil, err
			}
			HandOver(live, current.ID, next)
			if object, ok := live.(map[string]any); ok {
				delete(object, "generation")
			}
			return result, nil
		}
		done := "done"
		if err := EndRun(tx, live, current.ID, SubmissionSettlement{Status: SubmissionDone, Answer: &entry.ID, Reason: &done}); err != nil {
			return nil, err
		}
		if len(applied.Users) > 0 {
			if err := StartRun(tx, conversationID, live, applied.Users); err != nil {
				return nil, err
			}
		}
		return result, nil
	}, ctx)
}

// startToolRound appends the tool-calling answer and starts its round.
func startToolRound(runtime TaskRuntime, request generationRequestInfo, message *ai.AssistantMessage, calls []ai.ToolCall, ctx chord.Context) error {
	conversationID := runtime.ConversationID()
	messages := request.Messages
	if messages == nil {
		view, err := runtime.Context(conversationID, ctx, request.Cutoff)
		if err != nil {
			return err
		}
		messages = view.Messages
	}
	offered := map[string]bool{}
	for _, tool := range ai.GetCurrentTools(messages) {
		offered[tool.Name] = true
	}
	agent, err := runtime.Agent(ctx)
	if err != nil {
		return err
	}
	sequential := runtime.Settings().ToolExecution == ToolExecutionSequential
	if !sequential {
		for _, call := range calls {
			if offered[call.Name] {
				if tool := findTool(agent.Tools, call.Name); tool != nil && tool.ExecutionMode == ToolExecutionSequential {
					sequential = true
					break
				}
			}
		}
	}
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		live, err := tx.Doc(LiveDoc.Definition, conversationID)
		if err != nil {
			return nil, err
		}
		entry, err := appendAssistant(tx, conversationID, message)
		if err != nil {
			return nil, err
		}
		slots := []any{}
		toolIDs := []Id{}
		pending := []string{}
		for _, call := range calls {
			if !offered[call.Name] {
				unavailable := HarnessError("tool_unavailable", "Tool "+call.Name+" is not available")
				result, err := AppendToolResult(tx, conversationID, call, unavailable, runtime.Now())
				if err != nil {
					return nil, err
				}
				slots = append(slots, map[string]any{"callId": call.ID, "name": call.Name, "status": ToolSlotDone, "entry": result.ID})
				continue
			}
			if sequential && len(toolIDs) > 0 {
				pending = append(pending, call.ID)
				slots = append(slots, map[string]any{"callId": call.ID, "name": call.Name, "status": ToolSlotPending})
				continue
			}
			taskID, err := CreateToolTask(tx, current.ID, entry.ID, call.ID)
			if err != nil {
				return nil, err
			}
			toolIDs = append(toolIDs, taskID)
			slots = append(slots, map[string]any{"callId": call.ID, "name": call.Name, "taskId": taskID, "status": ToolSlotPending})
		}
		if object, ok := live.(map[string]any); ok {
			delete(object, "generation")
			object["tools"] = slots
		}
		payload := map[string]any{"phase": GenerationPhaseTools, "assistant": entry.ID, "tools": toolIDs, "pending": pending}
		encoded, err := marshalJSONValue(payload)
		if err != nil {
			return nil, err
		}
		return &NextTaskState{Status: TaskWaiting, Checkpoint: json.RawMessage(encoded), On: toolIDs, Policy: JoinAllSettled}, nil
	}, ctx)
}

// finishToolRound applies the round's controls and either ends the run or
// hands it to the next generation.
func finishToolRound(runtime TaskRuntime, assistant Id, toolIDs []Id, ctx chord.Context) error {
	conversationID := runtime.ConversationID()
	controls := map[Id]*ToolControl{}
	outcomes, err := runtime.Outcomes(toolIDs, ctx)
	if err != nil {
		return err
	}
	for index, id := range toolIDs {
		if index < len(outcomes) && outcomes[index].Status == OutcomeCompleted {
			var result ToolTaskResult
			_ = json.Unmarshal(outcomes[index].Result, &result)
			controls[id] = result.Control
		}
	}
	var slots []any
	if value, present, err := runtime.Snapshot(ctx, LiveDoc.Definition, conversationID); err != nil {
		return err
	} else if present {
		if object, ok := value.(map[string]any); ok {
			slots, _ = object["tools"].([]any)
		}
	}
	results := []Id{}
	for _, item := range slots {
		if slot, ok := item.(map[string]any); ok {
			if entry, ok := jsonID(slot["entry"]); ok {
				results = append(results, entry)
			}
		}
	}
	if err := runtime.Hooks().Each("afterTools", func(handler any) error {
		hooks, ok := handler.(GenerationHooks)
		if !ok || hooks.AfterTools == nil {
			return nil
		}
		return hooks.AfterTools(assistant, results, runtime, ctx)
	}); err != nil {
		return err
	}
	terminate := len(slots) > 0
	for _, item := range slots {
		slot, ok := item.(map[string]any)
		if !ok {
			terminate = false
			break
		}
		taskID, hasTask := jsonID(slot["taskId"])
		if !hasTask {
			terminate = false
			break
		}
		control := controls[taskID]
		if control == nil || !control.Terminate {
			terminate = false
			break
		}
	}
	added := []string{}
	for _, id := range toolIDs {
		if control := controls[id]; control != nil {
			added = append(added, control.AddTools...)
		}
	}
	var handoff *string
	for _, id := range toolIDs {
		if control := controls[id]; control != nil && control.Handoff != nil {
			handoff = control.Handoff
		}
	}
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		settings := runtime.Settings()
		boundary, err := PrepareBoundary(tx, conversationID, QueueModes{SteeringMode: settings.SteeringMode, FollowUpMode: settings.FollowUpMode})
		if err != nil {
			return nil, err
		}
		if len(added) > 0 {
			if err := AddTools(tx, conversationID, added); err != nil {
				return nil, err
			}
		}
		live, err := tx.Doc(LiveDoc.Definition, conversationID)
		if err != nil {
			return nil, err
		}
		now := runtime.Now()
		if terminate || handoff != nil {
			if handoff != nil {
				userMessage, err := userMessageFromContent(*handoff, now)
				if err != nil {
					return nil, err
				}
				entry, err := tx.AppendEntry(conversationID, EntryDraft{Kind: ResetEntry.Kind, HeadSelf: true, Model: []ai.Message{userMessage}})
				if err != nil {
					return nil, err
				}
				head := entry.ID
				boundary.Head = &head
			}
			applied, err := ApplyBoundary(tx, boundary, "final", now)
			if err != nil {
				return nil, err
			}
			done := "done"
			if err := EndRun(tx, live, current.ID, SubmissionSettlement{Status: SubmissionDone, Answer: &assistant, Reason: &done}); err != nil {
				return nil, err
			}
			if len(applied.Users) > 0 {
				if err := StartRun(tx, conversationID, live, applied.Users); err != nil {
					return nil, err
				}
			}
		} else {
			applied, err := ApplyBoundary(tx, boundary, "postTools", now)
			if err != nil {
				return nil, err
			}
			if applied.Reset {
				reason := "reset"
				if err := EndRun(tx, live, current.ID, SubmissionSettlement{Status: SubmissionUnanswered, Reason: &reason}); err != nil {
					return nil, err
				}
				if len(applied.Users) > 0 {
					if err := StartRun(tx, conversationID, live, applied.Users); err != nil {
						return nil, err
					}
				}
			} else {
				if object, ok := live.(map[string]any); ok {
					delete(object, "tools")
					if run, ok := object["run"].(map[string]any); ok {
						if runTaskID, ok := jsonID(run["taskId"]); ok && runTaskID == current.ID {
							inputs, _ := run["inputs"].([]any)
							for _, id := range applied.Users {
								inputs = append(inputs, id)
							}
							run["inputs"] = inputs
						}
					}
				}
				next, err := CreateGeneration(tx, conversationID)
				if err != nil {
					return nil, err
				}
				HandOver(live, current.ID, next)
			}
		}
		return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{
			Status: OutcomeCompleted, Result: dataJSON(map[string]any{"entryId": assistant}),
		}}, nil
	}, ctx)
}

// streamGenerationResponse streams one request, committing partials at most
// every 100 ms, and returns the terminal message.
func streamGenerationResponse(runtime TaskRuntime, model *ai.Model, messages []ai.Message, options ai.SimpleStreamOptions, attempt int, ctx chord.Context) (*ai.AssistantMessage, error) {
	if runtime.Models() == nil {
		return nil, fmt.Errorf("no models are configured")
	}
	stream := runtime.Models().StreamSimple(model, ai.Context{Messages: messages}, &ai.ModelsSimpleStreamOptions{SimpleStreamOptions: options})
	var lastFlush time.Time
	for {
		event, ok := stream.Next(ctx)
		if !ok {
			break
		}
		if event.Type == ai.EventDone || event.Type == ai.EventError {
			continue
		}
		if event.Partial == nil || len(event.Partial.Content) == 0 {
			continue
		}
		partial := event.Partial
		if !lastFlush.IsZero() && time.Since(lastFlush) < 100*time.Millisecond {
			continue
		}
		raw, err := ai.MarshalMessage(partial)
		if err != nil {
			continue
		}
		var value chord.JsonValue
		if err := json.Unmarshal(raw, &value); err != nil {
			continue
		}
		if err := runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
			live, err := tx.Doc(LiveDoc.Definition, runtime.ConversationID())
			if err != nil {
				return nil, err
			}
			object, ok := live.(map[string]any)
			if !ok {
				return nil, nil
			}
			generation, ok := object["generation"].(map[string]any)
			if !ok {
				generation = map[string]any{"attempt": attempt}
				object["generation"] = generation
			}
			AssignJSON(generation, "message", value)
			return nil, nil
		}, ctx); err != nil {
			if runtime.Signal().Err() != nil {
				return nil, err
			}
			runtime.Report(err)
		}
		lastFlush = time.Now()
	}
	message, err := stream.Result(ctx)
	if err != nil {
		return nil, err
	}
	return message, nil
}

// generationStreamOptions forwards the curated options into a pi-ai request.
func generationStreamOptions(stream ConversationStreamOptions, thinkingLevel string, signal context.Context) ai.SimpleStreamOptions {
	options := ai.SimpleStreamOptions{}
	options.Ctx = signal
	if stream.Transport != nil {
		options.Transport = ai.Transport(*stream.Transport)
	}
	options.TimeoutMs = stream.TimeoutMs
	options.MaxRetries = stream.MaxRetries
	options.MaxRetryDelayMs = stream.MaxRetryDelayMs
	if stream.Headers != nil {
		headers := ai.ProviderHeaders{}
		for key, value := range stream.Headers {
			text := value
			headers[key] = &text
		}
		options.Headers = headers
	}
	if stream.Metadata != nil {
		if encoded, err := marshalJSONValue(stream.Metadata); err == nil {
			var metadata map[string]json.RawMessage
			if err := json.Unmarshal([]byte(encoded), &metadata); err == nil {
				options.Metadata = metadata
			}
		}
	}
	if stream.CacheRetention != nil {
		options.CacheRetention = ai.CacheRetention(*stream.CacheRetention)
	}
	if thinkingLevel != "" && thinkingLevel != "off" {
		options.Reasoning = ai.ThinkingLevel(thinkingLevel)
	}
	return options
}
