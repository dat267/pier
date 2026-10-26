package coding

import (
	"encoding/json"
	"github.com/dat267/pier/ai"
	"strings"
	"testing"
)

// D264 keeps the pinned compaction prompts but escalates length stops by
// splitting. When every boundary separates a call/result pair, its documented
// fallback still needs two nonempty chunks, even if the result dominates weight.
// Reference: pi v1.4.0 (4a080252), core/compaction/compaction.ts compact.
func TestCompactionSplitsResultHeavyToolPairAtLastResort(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIOpenAICompletions, Provider: "test", ContextWindow: 100000}
	var prompts []string
	streamFn := func(model *ai.Model, transcript ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		prompt := ""
		for _, message := range transcript.Messages {
			if user, ok := message.(*ai.UserMessage); ok {
				prompt = ai.ContentText(user.Content, "")
			}
		}
		prompts = append(prompts, prompt)
		reason := ai.StopStop
		text := "checkpoint"
		// A provider with room for one source message can summarize either chunk,
		// but returns an incomplete answer when both are sent at once.
		if strings.Contains(prompt, "[Assistant tool calls]:") && strings.Contains(prompt, "[Tool result]:") {
			reason = ai.StopLength
			text = "partial"
		}
		stream := ai.NewAssistantMessageEventStream()
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: reason, Message: &ai.AssistantMessage{API: model.API, Provider: model.Provider, Model: model.ID, StopReason: reason, Content: ai.ContentList{ai.TextContent{Text: text}}, Usage: ai.Usage{Input: 1, TotalTokens: 1}}})
		return stream
	}
	preparation := &CompactionPreparation{
		FirstKeptEntryID: "kept", FileOps: CreateFileOps(),
		MessagesToSummarize: []ai.Message{
			&ai.AssistantMessage{Content: ai.ContentList{ai.ToolCall{ID: "call", Name: "read", Arguments: json.RawMessage(`{"path":"file"}`)}}, StopReason: ai.StopToolUse},
			&ai.ToolResultMessage{ToolCallID: "call", ToolName: "read", Content: ai.UserContentList{ai.TextContent{Text: strings.Repeat("x", 4000)}}},
		},
	}
	result, err := Compact(preparation, CompactionOptions{Model: model, StreamFn: streamFn})
	if err != nil {
		t.Fatalf("result-heavy pair could be summarized as two chunks: %v (requests=%d)", err, len(prompts))
	}
	if len(prompts) != 3 {
		t.Fatalf("requests = %d, want whole input then two chunks", len(prompts))
	}
	if !strings.Contains(prompts[2], "<previous-summary>\ncheckpoint\n</previous-summary>") {
		t.Fatal("last chunk lost the first checkpoint")
	}
	if result.Usage == nil || result.Usage.Input != 3 {
		t.Fatalf("usage = %+v, want all three requests", result.Usage)
	}
}
