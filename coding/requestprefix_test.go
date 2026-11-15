package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// D419: the capture remembers one outbound request for the compaction replay.
// It copies the message slice, so a caller that appends to its own transcript
// afterwards does not change what was captured.
func TestRequestPrefixCaptureCopiesTheRequest(t *testing.T) {
	capture := &requestPrefixCapture{}
	model := &ai.Model{ID: "m", Provider: "anthropic"}
	first := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}}
	messages := []ai.Message{first}
	capture.record(model, ai.TranscriptContext{Messages: messages}, &ai.SimpleStreamOptions{
		StreamOptions: ai.StreamOptions{CacheRetention: ai.CacheRetentionLong},
		Reasoning:     ai.ThinkHigh,
	})
	messages = append(messages, &ai.UserMessage{Content: ai.StringOrBlocks{Text: "second"}})

	captured := capture.snapshot()
	if captured == nil || len(captured.Messages) != 1 || captured.Messages[0] != first {
		t.Fatalf("captured = %+v, want the single message as sent", captured)
	}
	if captured.Model != model || captured.Reasoning != ai.ThinkHigh || captured.CacheRetention != ai.CacheRetentionLong {
		t.Fatalf("captured options = %+v", captured)
	}

	// Nothing to capture without a model or messages.
	capture.record(nil, ai.TranscriptContext{Messages: messages}, nil)
	if len(capture.snapshot().Messages) != 1 {
		t.Fatal("a nil model overwrote the capture")
	}
	capture.record(model, ai.TranscriptContext{}, nil)
	if len(capture.snapshot().Messages) != 1 {
		t.Fatal("an empty transcript overwrote the capture")
	}

	capture.clear()
	if capture.snapshot() != nil {
		t.Fatal("clear kept the capture")
	}
	var nilCapture *requestPrefixCapture
	nilCapture.record(model, ai.TranscriptContext{Messages: messages}, nil)
	nilCapture.clear()
	if nilCapture.snapshot() != nil {
		t.Fatal("a nil capture must be inert")
	}
}

// D419: ConvertToLlm allocates a fresh message for every custom message, so the
// live context holds a different pointer for the same conversation; the replay
// compares the wire content, not the pointer.
func TestSameWireMessageComparesContent(t *testing.T) {
	left := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "same"}, Timestamp: 7}
	right := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "same"}, Timestamp: 7}
	if !sameWireMessage(left, right) {
		t.Fatal("equal messages from separate allocations compare unequal")
	}
	if !sameWireMessage(left, left) {
		t.Fatal("a message must equal itself")
	}
	changed := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "different"}, Timestamp: 7}
	if sameWireMessage(left, changed) {
		t.Fatal("different content compared equal")
	}
}

// A context that ends in an unanswered tool call cannot be replayed as a
// prompt: the provider expects the tool results next.
func TestEndsWithToolCall(t *testing.T) {
	call := &ai.AssistantMessage{Content: ai.ContentList{
		ai.TextContent{Text: "running"},
		ai.ToolCall{ID: "1", Name: "read"},
	}}
	if !endsWithToolCall(call) {
		t.Fatal("an assistant message with a tool call must be rejected")
	}
	textOnly := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "done"}}}
	if endsWithToolCall(textOnly) {
		t.Fatal("a text-only assistant message was rejected")
	}
	if endsWithToolCall(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}}) {
		t.Fatal("a user message was treated as a tool call")
	}
}
