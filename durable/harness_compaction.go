package durable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dat267/pier/ai"
)

// Port of harness/compaction.ts: the compaction task's input/checkpoint types,
// the summarization prompts, and the pure range-selection and serialization
// helpers. The task's phases land with the task runtime.

// CompactionInput is the compaction task input.
type CompactionInput struct {
	Reason       CompactionReason `json:"reason"`
	Instructions *string          `json:"instructions,omitempty"`
}

// SummaryRequest is the pinned summarization request.
type SummaryRequest struct {
	Attempt       int                       `json:"attempt"`
	Model         ModelRef                  `json:"model"`
	ThinkingLevel string                    `json:"thinkingLevel"`
	StreamOptions ConversationStreamOptions `json:"streamOptions"`
	MaxTokens     int                       `json:"maxTokens"`
	Tail          Id                        `json:"tail"`
	FirstKept     Id                        `json:"firstKept"`
}

// Compaction checkpoint phases.
const (
	CompactionPhaseSelect    = "select"
	CompactionPhaseSummarize = "summarize"
	CompactionPhaseRetry     = "retry"
)

// CompactionCheckpoint is the compaction task's durable checkpoint.
type CompactionCheckpoint struct {
	Phase string `json:"phase"`
	SummaryRequest
	Until int64 `json:"until,omitempty"`
}

// ToolResultMaxChars is the longest tool result text a serialized summary
// source keeps.
const ToolResultMaxChars = 2000

// SummaryPrefix and SummarySuffix wrap a placed summary entry.
const (
	SummaryPrefix = "The conversation history before this point was compacted into the following summary:\n\n<summary>\n"
	SummarySuffix = "\n</summary>"
)

// SummarizationSystemPrompt and SummarizationPrompt are the summarizer's
// instructions.
const (
	SummarizationSystemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`

	SummarizationPrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work. If the conversation starts with an earlier summary, preserve its information and fold the newer messages into it.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`
)

// CompactionTask is the built-in compaction task definition.
var CompactionTask = Task{Definition: TaskDefinition{
	Name: CompactionTaskKind, Version: 1,
	Initial: func(json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"phase":"select"}`), nil
	},
}}

// Threshold compaction outcomes.
const (
	ThresholdBlocking   = "blocking"
	ThresholdBackground = "background"
)

// ThresholdCompaction is which threshold compaction preparation starts before
// its request (spec §8.3), or "" for none.
func ThresholdCompaction(view ContextView, planned []EntryDraft, contextWindow int, policy CompactionPolicy) string {
	if !policy.Enabled || contextWindow <= 0 {
		return ""
	}
	extra := []ai.Message{}
	for _, entry := range planned {
		extra = append(extra, entry.Model...)
	}
	tokens := EstimateContext(view, extra)
	blocking := contextWindow - policy.ReserveTokens
	background := blocking - policy.BackgroundTokens
	over := ""
	switch {
	case tokens > blocking:
		over = ThresholdBlocking
	case policy.BackgroundTokens > 0 && tokens > background:
		over = ThresholdBackground
	}
	if over == "" || SelectCut(view, policy.KeepRecentTokens) == nil {
		return ""
	}
	return over
}

// CreateCompaction creates a compaction task with its status in this commit.
// owner is the generation that waits for it; without one the task is
// conversation-owned and background unless manual.
func CreateCompaction(tx *Transaction, conversationID Id, input CompactionInput, owner *Id) (Id, error) {
	ownership := TaskOwnership{Kind: TaskOwnedByConversation}
	if owner != nil {
		ownership = TaskOwnership{Kind: TaskOwnedByTask, TaskID: owner}
	}
	background := owner == nil && input.Reason != CompactionManual
	encoded, err := marshalJSONValue(input)
	if err != nil {
		return 0, err
	}
	taskID, err := tx.CreateTask(CompactionTask.Definition, json.RawMessage(encoded), TaskOptions{
		Ownership: ownership, ConversationID: &conversationID, Background: background,
	})
	if err != nil {
		return 0, err
	}
	live, err := tx.Doc(LiveDoc.Definition, conversationID)
	if err != nil {
		return 0, err
	}
	if err := AddCompactionStatus(live, CompactionStatus{
		TaskID: taskID, Reason: input.Reason, Blocking: owner != nil, Attempt: 1,
	}); err != nil {
		return 0, err
	}
	return taskID, nil
}

// SelectCut is the index in view.Entries of the first entry a summary keeps,
// or nil when there is nothing to compact (spec §8.7).
func SelectCut(view ContextView, keepRecentTokens int) *int {
	contributions := view.Contributions
	start := 0
	if view.Head != nil {
		start = 1
	}
	candidates := []int{}
	for index := start; index < len(contributions); index++ {
		if isCutCandidate(contributions, index) {
			candidates = append(candidates, index)
		}
	}
	kept := 0
	var cut *int
	for index := len(contributions) - 1; index >= start; index-- {
		for _, message := range contributions[index] {
			kept += ai.EstimateMessageTokens(message)
		}
		if kept < keepRecentTokens {
			continue
		}
		var chosen *int
		for _, candidate := range candidates {
			if candidate >= index {
				value := candidate
				chosen = &value
				break
			}
		}
		if chosen == nil && len(candidates) > 0 {
			value := candidates[len(candidates)-1]
			chosen = &value
		}
		cut = chosen
		break
	}
	if cut == nil {
		return nil
	}
	for index := start; index < *cut; index++ {
		if len(contributions[index]) > 0 {
			return cut
		}
	}
	return nil
}

func isCutCandidate(contributions [][]ai.Message, index int) bool {
	if len(contributions[index]) == 0 {
		return false
	}
	first := contributions[index][0]
	if _, ok := first.(*ai.AssistantMessage); ok {
		return true
	}
	if _, ok := first.(*ai.UserMessage); !ok {
		return false
	}
	// A result of the preceding assistant's calls that follows this entry,
	// before the next assistant, belongs before it.
	calls := map[string]bool{}
	for before := index - 1; before >= 0; before-- {
		assistant := lastAssistant(contributions[before])
		if assistant == nil {
			continue
		}
		for _, content := range assistant.Content {
			if call, ok := content.(ai.ToolCall); ok {
				calls[call.ID] = true
			}
		}
		break
	}
	if len(calls) == 0 {
		return true
	}
	for after := index; after < len(contributions); after++ {
		for position, message := range contributions[after] {
			if _, ok := message.(*ai.AssistantMessage); ok && (after > index || position > 0) {
				return true
			}
			if result, ok := message.(*ai.ToolResultMessage); ok && calls[result.ToolCallID] {
				return false
			}
		}
	}
	return true
}

func lastAssistant(messages []ai.Message) *ai.AssistantMessage {
	for index := len(messages) - 1; index >= 0; index-- {
		if assistant, ok := messages[index].(*ai.AssistantMessage); ok {
			return assistant
		}
	}
	return nil
}

// SummarizedMessages is the model messages of the entries before cut: the head
// marker first, ordered like model context.
func SummarizedMessages(view ContextView, cut int) []ai.Message {
	grouped := view.Contributions[:cut]
	return OrderToolResults(flattenMessages(grouped))
}

// EstimateContext is the size of a request over view followed by extra
// (spec §8.3).
func EstimateContext(view ContextView, extra []ai.Message) int {
	var measured *ai.AssistantMessage
	after := int64(-1 << 62)
	if view.Head != nil {
		after = view.Head.ID
	}
	for index := len(view.Entries) - 1; index >= 0 && measured == nil; index-- {
		if view.Entries[index].ID <= after {
			continue
		}
		for position := len(view.Contributions[index]) - 1; position >= 0; position-- {
			if assistant, ok := view.Contributions[index][position].(*ai.AssistantMessage); ok &&
				ai.CalculateContextTokens(assistant.Usage) > 0 {
				measured = assistant
				break
			}
		}
	}
	from := 0
	tokens := 0
	if measured != nil {
		from = len(view.Messages)
		for index, message := range view.Messages {
			if message == measured {
				from = index + 1
			}
		}
		tokens = ai.CalculateContextTokens(measured.Usage)
	}
	for _, message := range view.Messages[from:] {
		tokens += ai.EstimateMessageTokens(message)
	}
	for _, message := range extra {
		tokens += ai.EstimateMessageTokens(message)
	}
	return tokens
}

// SummaryText is the summary of a clean stop with text and no tool call.
func SummaryText(message *ai.AssistantMessage) (string, bool) {
	if message.StopReason != ai.StopStop {
		return "", false
	}
	texts := []string{}
	for _, content := range message.Content {
		if _, ok := content.(ai.ToolCall); ok {
			return "", false
		}
		if text, ok := content.(ai.TextContent); ok {
			texts = append(texts, text.Text)
		}
	}
	text := strings.TrimSpace(strings.Join(texts, "\n"))
	if text == "" {
		return "", false
	}
	return text, true
}

// SummaryFailure explains why a message is not a usable summary.
func SummaryFailure(message *ai.AssistantMessage) string {
	if message.StopReason == ai.StopError || message.StopReason == ai.StopAborted {
		if message.ErrorMessage != nil {
			return "Summarization failed: " + *message.ErrorMessage
		}
		return "Summarization failed: " + message.StopReason
	}
	if message.StopReason == ai.StopLength {
		return "Summarization hit the token limit; the summary is incomplete"
	}
	for _, content := range message.Content {
		if _, ok := content.(ai.ToolCall); ok {
			return "Summarization attempted to call a tool"
		}
	}
	return "Summarization produced no text"
}

// SummaryPrompt is the summarizer's user message: the serialized conversation,
// the prompt, and any instructions.
func SummaryPrompt(messages []ai.Message, instructions *string) string {
	focus := ""
	if instructions != nil {
		focus = "\n\nAdditional focus: " + *instructions
	}
	return "<conversation>\n" + SerializeConversation(messages) + "\n</conversation>\n\n" + SummarizationPrompt + focus
}

// SerializeConversation renders messages as plain text, so the summarizer
// reads a transcript instead of continuing it. System messages are omitted.
func SerializeConversation(messages []ai.Message) string {
	parts := []string{}
	for _, message := range messages {
		switch typed := message.(type) {
		case *ai.UserMessage:
			if text := userContentText(typed.Content); text != "" {
				parts = append(parts, "[User]: "+text)
			}
		case *ai.AssistantMessage:
			thinking := []string{}
			text := []string{}
			calls := []string{}
			for _, content := range typed.Content {
				switch block := content.(type) {
				case ai.ThinkingContent:
					thinking = append(thinking, block.Thinking)
				case ai.TextContent:
					text = append(text, block.Text)
				case ai.ToolCall:
					calls = append(calls, block.Name+"("+argumentsText(block.Arguments)+")")
				}
			}
			if len(thinking) > 0 {
				parts = append(parts, "[Assistant thinking]: "+strings.Join(thinking, "\n"))
			}
			if len(text) > 0 {
				parts = append(parts, "[Assistant]: "+strings.Join(text, "\n"))
			}
			if len(calls) > 0 {
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(calls, "; "))
			}
		case *ai.ToolResultMessage:
			if text := blocksText(typed.Content); text != "" {
				parts = append(parts, "[Tool result]: "+truncateText(text, ToolResultMaxChars))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func userContentText(content ai.StringOrBlocks) string {
	if content.String() {
		return content.Text
	}
	texts := []string{}
	for _, block := range content.Blocks {
		if text, ok := block.(ai.TextContent); ok {
			texts = append(texts, text.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func blocksText(content []ai.UserContent) string {
	texts := []string{}
	for _, block := range content {
		if text, ok := block.(ai.TextContent); ok {
			texts = append(texts, text.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func truncateText(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	return text[:maxChars] + fmt.Sprintf("\n\n[... %d more characters truncated]", len(text)-maxChars)
}

// argumentsText renders tool call arguments in insertion order.
func argumentsText(raw json.RawMessage) string {
	entries, err := orderedObjectEntries(raw)
	if err != nil {
		return ""
	}
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, entry[0]+"="+entry[1])
	}
	return strings.Join(parts, ", ")
}

func orderedObjectEntries(raw json.RawMessage) ([][2]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("not an object")
	}
	entries := [][2]string{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyToken.(string)
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		compacted := &bytes.Buffer{}
		if err := json.Compact(compacted, value); err != nil {
			return nil, err
		}
		entries = append(entries, [2]string{key, compacted.String()})
	}
	return entries, nil
}
