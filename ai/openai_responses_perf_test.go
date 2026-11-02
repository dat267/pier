package ai

import (
	"encoding/json"
	"fmt"
	"testing"
)

// A large Responses request: one user, tool-call assistant, tool result and
// final answer per turn. Upstream builds plain objects and stringifies once.
func largeResponsesMessages(turns int) []Message {
	messages := make([]Message, 0, 4*turns)
	for i := 0; i < turns; i++ {
		callID := fmt.Sprintf("call_%d", i)
		messages = append(messages, &UserMessage{Content: StringOrBlocks{Text: "Please inspect the code and explain."}, Timestamp: int64(i)})
		messages = append(messages, &AssistantMessage{
			API: APIOpenAIResponses, Provider: "openai", Model: "gpt-5",
			Content: ContentList{
				TextContent{Text: "Here is the answer."},
				ToolCall{ID: callID, Name: "read", Arguments: json.RawMessage(`{"path":"a.go"}`)},
			},
			StopReason: StopToolUse, Timestamp: int64(i) + 1,
		})
		messages = append(messages, &ToolResultMessage{ToolCallID: callID, ToolName: "read", Content: UserContentList{TextContent{Text: "package main"}}, Timestamp: int64(i) + 2})
		messages = append(messages, &AssistantMessage{API: APIOpenAIResponses, Provider: "openai", Model: "gpt-5", Content: ContentList{TextContent{Text: "done"}}, StopReason: StopStop, Timestamp: int64(i) + 3})
	}
	return messages
}

func BenchmarkConvertResponsesMessages(b *testing.B) {
	model := &Model{ID: "gpt-5", Provider: "openai", API: APIOpenAIResponses, ContextWindow: 400000}
	messages := largeResponsesMessages(10000)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ConvertResponsesMessages(model, TranscriptContext{Messages: messages}, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// The direct item assembly must stay canonical: anything the emitter copies
// verbatim (reasoning signatures) is canonicalized first.
func TestConvertResponsesMessagesOutputIsCanonical(t *testing.T) {
	model := &Model{ID: "gpt-5", Provider: "openai", API: APIOpenAIResponses, ContextWindow: 400000}
	signature := `{"2":1,"1":"\u2028","1":"dup"}`
	messages := []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
		&AssistantMessage{
			API: APIOpenAIResponses, Provider: "openai", Model: "gpt-5",
			Content:    ContentList{ThinkingContent{Thinking: "hmm", ThinkingSignature: &signature}},
			StopReason: StopStop, Timestamp: 2,
		},
	}
	encoded, err := ConvertResponsesMessages(model, TranscriptContext{Messages: messages}, nil)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalJavaScriptJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != string(encoded) {
		t.Fatalf("output is not canonical:\n got %s\nwant %s", encoded, canonical)
	}
}

func TestConvertResponsesMessagesAllocationBudget(t *testing.T) {
	model := &Model{ID: "gpt-5", Provider: "openai", API: APIOpenAIResponses, ContextWindow: 400000}
	messages := largeResponsesMessages(500)
	allocs := testing.AllocsPerRun(2, func() {
		if _, err := ConvertResponsesMessages(model, TranscriptContext{Messages: messages}, nil); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 60000 {
		t.Fatalf("conversion allocated %.0f objects, want <= 60000", allocs)
	}
}
