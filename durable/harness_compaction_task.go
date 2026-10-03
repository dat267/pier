package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of the built-in compaction task's phases (harness/compaction.ts).

func compactionInputOf(raw json.RawMessage) CompactionInput {
	var input CompactionInput
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &input)
	}
	return input
}

func compactionCheckpointOf(raw json.RawMessage) CompactionCheckpoint {
	var checkpoint CompactionCheckpoint
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &checkpoint)
	}
	return checkpoint
}

func dataJSON(value any) json.RawMessage {
	encoded, err := marshalJSONValue(value)
	if err != nil {
		return nil
	}
	return json.RawMessage(encoded)
}

// compactionSelect renders the range the summary replaces and moves to the
// summarize phase, or completes when there is nothing to compact.
func compactionSelect(task RunningTask, runtime TaskRuntime, ctx chord.Context) error {
	conversationID := runtime.ConversationID()
	agent, err := runtime.Agent(ctx)
	if err != nil {
		return err
	}
	settings := runtime.Settings()
	ref := agent.Model
	var model *ai.Model
	if ref != nil && runtime.Models() != nil {
		model = runtime.Models().GetModel(ref.Provider, ref.ModelID)
	}
	if ref == nil || model == nil {
		return compactionFailNoModel(runtime, ref, ctx)
	}
	policy := settings.Compaction
	view, err := runtime.Context(conversationID, ctx, nil)
	if err != nil {
		return err
	}
	cut := SelectCut(view, policy.KeepRecentTokens)
	if cut == nil {
		return compactionComplete(runtime, ctx)
	}
	firstKept := view.Entries[*cut].ID
	input := compactionInputOf(task.Input)
	compaction := CompactionHookRequest{
		Reason: input.Reason, Entries: view.Entries[:*cut], Messages: SummarizedMessages(view, *cut),
		FirstKept: firstKept, Instructions: input.Instructions,
	}
	var decision *CompactionDecision
	hookErr := runtime.Hooks().Each("beforeCompact", func(handler any) error {
		if decision != nil {
			return nil
		}
		hooks, ok := handler.(CompactionHooks)
		if !ok || hooks.BeforeCompact == nil {
			return nil
		}
		result, err := hooks.BeforeCompact(compaction, runtime, ctx)
		if err != nil {
			return err
		}
		decision = result
		return nil
	})
	if hookErr != nil {
		return hookErr
	}
	if decision != nil && decision.Decline {
		return compactionComplete(runtime, ctx)
	}
	if decision != nil && decision.Summary != nil {
		return placeCompaction(runtime, firstKept, *decision.Summary, ctx)
	}
	request := SummaryRequest{
		Attempt: 1, Model: *ref, ThinkingLevel: agent.ThinkingLevel, StreamOptions: settings.Stream,
		MaxTokens: minInt(int(math.Floor(0.8*float64(policy.ReserveTokens))), summaryMaxTokens(model)),
		Tail:      tailEntry(view.Entries, firstKept), FirstKept: firstKept,
	}
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		encoded, err := marshalJSONValue(map[string]any{"phase": CompactionPhaseSummarize, "attempt": request.Attempt, "model": request.Model, "thinkingLevel": request.ThinkingLevel, "streamOptions": request.StreamOptions, "maxTokens": request.MaxTokens, "tail": request.Tail, "firstKept": request.FirstKept})
		if err != nil {
			return nil, err
		}
		return &NextTaskState{Status: TaskRunning, Checkpoint: json.RawMessage(encoded)}, nil
	}, ctx)
}

func summaryMaxTokens(model *ai.Model) int {
	if model.MaxTokens > 0 {
		return int(model.MaxTokens)
	}
	return math.MaxInt
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func tailEntry(entries []EntryRecord, firstKept Id) Id {
	tail := firstKept
	for _, entry := range entries {
		if entry.ID > tail {
			tail = entry.ID
		}
	}
	return tail
}

// compactionSummarize pins the summarization request and classifies the
// result: a summary is placed, a retryable error backs off, anything else
// fails.
func compactionSummarize(task RunningTask, runtime TaskRuntime, ctx chord.Context) error {
	checkpoint := compactionCheckpointOf(task.State.Checkpoint)
	ref := checkpoint.Model
	var model *ai.Model
	if runtime.Models() != nil {
		model = runtime.Models().GetModel(ref.Provider, ref.ModelID)
	}
	if model == nil {
		return compactionFailNoModel(runtime, &ref, ctx)
	}
	tail := checkpoint.Tail
	view, err := runtime.Context(runtime.ConversationID(), ctx, &tail)
	if err != nil {
		return err
	}
	cut := -1
	for index, entry := range view.Entries {
		if entry.ID == checkpoint.FirstKept {
			cut = index
			break
		}
	}
	if cut < 0 {
		return fmt.Errorf("Compaction range lost its first kept entry %d", checkpoint.FirstKept)
	}
	now := runtime.Now()
	input := compactionInputOf(task.Input)
	messages := []ai.Message{
		&ai.SystemMessage{Content: ai.StringOrBlocks{Text: SummarizationSystemPrompt}, Timestamp: now},
		&ai.UserMessage{Content: ai.StringOrBlocks{Blocks: ai.ContentList{
			ai.TextContent{Text: summaryPrompt(SummarizedMessages(view, cut), input.Instructions)},
		}}, Timestamp: now},
	}
	if runtime.Models() == nil {
		return compactionFailNoModel(runtime, &ref, ctx)
	}
	options := summaryStreamOptions(checkpoint.StreamOptions, checkpoint.MaxTokens, checkpoint.ThinkingLevel, runtime.Signal())
	message, err := runtime.Models().CompleteSimple(model, ai.Context{Messages: messages}, &ai.ModelsSimpleStreamOptions{SimpleStreamOptions: options})
	if err != nil {
		return err
	}
	if err := runtime.Signal().Err(); err != nil {
		return err
	}
	summary, hasSummary := SummaryText(message)
	policy := runtime.Settings().Retry
	retry := message.StopReason == ai.StopError && ai.IsRetryableAssistantError(message) &&
		policy.Enabled && checkpoint.Attempt <= policy.MaxRetries
	until := int64(0)
	if retry {
		until = runtime.Now() + ai.RetryDelayMS(retryPolicyOf(policy), checkpoint.Attempt)
	}
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		if err := RecordUsage(ctx, tx, runtime.ConversationID(), UsageBucketModels,
			message.Provider+"/"+message.Model, message.Usage); err != nil {
			return nil, err
		}
		live, err := tx.Doc(LiveDoc.Definition, runtime.ConversationID())
		if err != nil {
			return nil, err
		}
		if hasSummary {
			return placeSummary(tx, runtime, current, live, checkpoint.FirstKept, summary)
		}
		if retry {
			if status := CompactionStatusOf(live, runtime.TaskID()); status != nil {
				status["retry"] = map[string]any{"at": until, "error": errorMessageOf(message)}
			}
			encoded, err := marshalJSONValue(map[string]any{"phase": CompactionPhaseRetry, "attempt": checkpoint.Attempt, "model": checkpoint.Model, "thinkingLevel": checkpoint.ThinkingLevel, "streamOptions": checkpoint.StreamOptions, "maxTokens": checkpoint.MaxTokens, "tail": checkpoint.Tail, "firstKept": checkpoint.FirstKept, "until": until})
			if err != nil {
				return nil, err
			}
			return &NextTaskState{Status: TaskRunning, Checkpoint: json.RawMessage(encoded)}, nil
		}
		RemoveCompactionStatus(live, runtime.TaskID())
		detail := dataJSON(map[string]any{"reason": "model_error"})
		return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{
			Status: OutcomeFailed, Error: &StoredError{Message: SummaryFailure(message), Detail: detail},
		}}, nil
	}, ctx)
}

func errorMessageOf(message *ai.AssistantMessage) string {
	if message.ErrorMessage != nil {
		return *message.ErrorMessage
	}
	return ""
}

// compactionRetry sleeps the durable backoff and returns to summarize with the
// next attempt.
func compactionRetry(task RunningTask, runtime TaskRuntime, ctx chord.Context) error {
	checkpoint := compactionCheckpointOf(task.State.Checkpoint)
	if err := runtime.Sleep(checkpoint.Until, ctx); err != nil {
		return err
	}
	attempt := checkpoint.Attempt + 1
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		live, err := tx.Doc(LiveDoc.Definition, runtime.ConversationID())
		if err != nil {
			return nil, err
		}
		if status := CompactionStatusOf(live, runtime.TaskID()); status != nil {
			status["attempt"] = attempt
			delete(status, "retry")
		}
		encoded, err := marshalJSONValue(map[string]any{"phase": CompactionPhaseSummarize, "attempt": attempt, "model": checkpoint.Model, "thinkingLevel": checkpoint.ThinkingLevel, "streamOptions": checkpoint.StreamOptions, "maxTokens": checkpoint.MaxTokens, "tail": checkpoint.Tail, "firstKept": checkpoint.FirstKept})
		if err != nil {
			return nil, err
		}
		return &NextTaskState{Status: TaskRunning, Checkpoint: json.RawMessage(encoded)}, nil
	}, ctx)
}

func compactionAbort(task RunningTask, runtime TaskRuntime, ctx chord.Context) error {
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		live, err := tx.Doc(LiveDoc.Definition, runtime.ConversationID())
		if err != nil {
			return nil, err
		}
		RemoveCompactionStatus(live, runtime.TaskID())
		return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{Status: OutcomeAborted}}, nil
	}, ctx)
}

func compactionComplete(runtime TaskRuntime, ctx chord.Context) error {
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		live, err := tx.Doc(LiveDoc.Definition, runtime.ConversationID())
		if err != nil {
			return nil, err
		}
		RemoveCompactionStatus(live, runtime.TaskID())
		return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{
			Status: OutcomeCompleted, Result: dataJSON(CompactionResult{}),
		}}, nil
	}, ctx)
}

func compactionFailNoModel(runtime TaskRuntime, ref *ModelRef, ctx chord.Context) error {
	message := "No model is configured"
	if ref != nil {
		message = fmt.Sprintf("Model %s/%s is not available", ref.Provider, ref.ModelID)
	}
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		live, err := tx.Doc(LiveDoc.Definition, runtime.ConversationID())
		if err != nil {
			return nil, err
		}
		RemoveCompactionStatus(live, runtime.TaskID())
		return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{
			Status: OutcomeFailed,
			Error:  &StoredError{Message: message, Detail: dataJSON(map[string]any{"reason": "no_model"})},
		}}, nil
	}, ctx)
}

// placeCompaction places a hook-supplied summary in its own commit.
func placeCompaction(runtime TaskRuntime, firstKept Id, summary string, ctx chord.Context) error {
	return runtime.Commit(func(tx *Transaction, current RunningTask) (*NextTaskState, error) {
		live, err := tx.Doc(LiveDoc.Definition, runtime.ConversationID())
		if err != nil {
			return nil, err
		}
		return placeSummary(tx, runtime, current, live, firstKept, summary)
	}, ctx)
}

// placeSummary places the summary entry and completes (spec §8.7).
func placeSummary(tx *Transaction, runtime TaskRuntime, current RunningTask, live chord.JsonValue, firstKept Id, summary string) (*NextTaskState, error) {
	RemoveCompactionStatus(live, runtime.TaskID())
	text := SummaryPrefix + summary + SummarySuffix
	input := compactionInputOf(current.Input)
	entry := EntryDraft{
		Kind: CompactionEntry.Kind, Head: &firstKept,
		Model: []ai.Message{&ai.UserMessage{Content: ai.StringOrBlocks{Blocks: ai.ContentList{
			ai.TextContent{Text: text},
		}}, Timestamp: runtime.Now()}},
		Data: dataJSON(map[string]any{"reason": input.Reason}),
	}
	result := CompactionResult{}
	if current.Owner == nil {
		settings := runtime.Settings()
		requestID := fmt.Sprintf("compaction:%d", runtime.TaskID())
		submissionID, err := AdmitSubmission(tx, runtime.ConversationID(),
			SubmissionDraft{RequestID: &requestID, Type: SubmissionTypeWrite, Entry: &entry},
			runtime.Now(), QueueModes{SteeringMode: settings.SteeringMode, FollowUpMode: settings.FollowUpMode})
		if err != nil {
			return nil, err
		}
		result.SubmissionID = &submissionID
	} else {
		appended, err := tx.AppendEntry(runtime.ConversationID(), entry)
		if err != nil {
			return nil, err
		}
		result.EntryID = &appended.ID
	}
	return &NextTaskState{Status: TaskTerminal, Outcome: &TaskOutcome{
		Status: OutcomeCompleted, Result: dataJSON(result),
	}}, nil
}

// summaryPrompt is the summarizer's user message.
func summaryPrompt(messages []ai.Message, instructions *string) string {
	focus := ""
	if instructions != nil {
		focus = "\n\nAdditional focus: " + *instructions
	}
	return "<conversation>\n" + SerializeConversation(messages) + "\n</conversation>\n\n" + SummarizationPrompt + focus
}

// retryPolicyOf converts the durable retry policy to the pi-ai shape.
func retryPolicyOf(policy ConversationRetryPolicy) ai.RetryPolicy {
	result := ai.RetryPolicy{Enabled: policy.Enabled, MaxRetries: policy.MaxRetries, BaseDelayMS: policy.BaseDelayMs}
	if policy.MaxAgentDelayMs != nil {
		result.MaxAgentDelayMS = int64(*policy.MaxAgentDelayMs)
	}
	return result
}

// summaryStreamOptions forwards the curated conversation options into a
// pi-ai request.
func summaryStreamOptions(stream ConversationStreamOptions, maxTokens int, thinkingLevel string, signal context.Context) ai.SimpleStreamOptions {
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
	options.CacheRetention = ai.CacheRetentionNone
	options.MaxTokens = &maxTokens
	if thinkingLevel != "" && thinkingLevel != "off" {
		options.Reasoning = ai.ThinkingLevel(thinkingLevel)
	}
	return options
}
