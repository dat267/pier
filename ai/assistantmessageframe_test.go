package ai

import (
	"encoding/json"
	"testing"
)

// TestAssistantMessageFrameRoundTrip drives the encoder with a full stream and
// reduces the frames back into the assistant message.
func TestAssistantMessageFrameRoundTrip(t *testing.T) {
	encoder := NewAssistantMessageFrameEncoder()
	message := &AssistantMessage{
		Content: ContentList{}, StopReason: StopPending,
		API: APIOpenAIResponses, Provider: "openai", Model: "m",
		Usage: Usage{Cost: UsageCost{}}, Timestamp: 1,
	}
	var frames []AssistantMessageFrame
	encode := func(event *AssistantMessageEvent) {
		t.Helper()
		frame, err := encoder.Encode(event)
		if err != nil {
			t.Fatalf("encode %s: %v", event.Type, err)
		}
		if frame != nil {
			frames = append(frames, *frame)
		}
	}

	encode(&AssistantMessageEvent{Type: EventStart, Partial: message})
	message.Content = append(message.Content, TextContent{})
	encode(&AssistantMessageEvent{Type: EventTextStart, ContentIndex: 0, Partial: message})
	message.Content[0] = TextContent{Text: "Hello"}
	encode(&AssistantMessageEvent{Type: EventTextDelta, ContentIndex: 0, Delta: "Hello", Partial: message})
	encode(&AssistantMessageEvent{Type: EventTextEnd, ContentIndex: 0, Content: "Hello", Partial: message})

	message.Content = append(message.Content, ToolCall{ID: "c1", Name: "read", Arguments: json.RawMessage("{}")})
	encode(&AssistantMessageEvent{Type: EventToolcallStart, ContentIndex: 1, Partial: message})
	message.Content[1] = ToolCall{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)}
	encode(&AssistantMessageEvent{Type: EventToolcallDelta, ContentIndex: 1, Delta: `{"path":"a"}`, Partial: message})
	final := ToolCall{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)}
	message.Content[1] = final
	encode(&AssistantMessageEvent{Type: EventToolcallEnd, ContentIndex: 1, ToolCall: &final, Partial: message})
	encode(&AssistantMessageEvent{Type: EventDone, Reason: StopStop, Message: message})

	reduced, err := ReduceAssistantMessageFrames(frames)
	if err != nil {
		t.Fatal(err)
	}
	if len(reduced.Content) != 2 {
		t.Fatalf("content = %+v", reduced.Content)
	}
	if text, ok := reduced.Content[0].(TextContent); !ok || text.Text != "Hello" {
		t.Fatalf("text = %+v", reduced.Content[0])
	}
	call, ok := reduced.Content[1].(ToolCall)
	if !ok || call.ID != "c1" || call.Name != "read" || string(call.Arguments) != `{"path":"a"}` {
		t.Fatalf("call = %+v", reduced.Content[1])
	}

	// The frames survive a JSON round-trip and still reduce to the same message.
	encoded, err := MarshalJSON(frames)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []AssistantMessageFrame
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	again, err := ReduceAssistantMessageFrames(decoded)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := MarshalJSON(again)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := MarshalJSON(reduced)
	if string(reencoded) != string(original) {
		t.Fatalf("round trip differs:\n %s\n %s", original, reencoded)
	}
}

// TestAssistantMessageFrameTerminalGuard pins that no frame follows a terminal
// event and done/error emit no frame.
func TestAssistantMessageFrameTerminalGuard(t *testing.T) {
	encoder := NewAssistantMessageFrameEncoder()
	message := &AssistantMessage{Content: ContentList{}, StopReason: StopPending, Usage: Usage{Cost: UsageCost{}}}
	frame, err := encoder.Encode(&AssistantMessageEvent{Type: EventStart, Partial: message})
	if err != nil || frame == nil || frame.Type != "start" {
		t.Fatalf("start = %+v, %v", frame, err)
	}
	if frame, err := encoder.Encode(&AssistantMessageEvent{Type: EventDone, Reason: StopStop, Message: message}); err != nil || frame != nil {
		t.Fatalf("done = %+v, %v", frame, err)
	}
	if _, err := encoder.Encode(&AssistantMessageEvent{Type: EventTextStart, ContentIndex: 0, Partial: message}); err == nil {
		t.Fatal("an event after done must fail")
	}
}

// TestReduceAssistantMessageFramesWithoutStart pins that a list without a start
// frame reduces to nil.
func TestReduceAssistantMessageFramesWithoutStart(t *testing.T) {
	message, err := ReduceAssistantMessageFrames([]AssistantMessageFrame{{Type: "text_delta", Delta: "x"}})
	if err != nil || message != nil {
		t.Fatalf("message = %+v, %v", message, err)
	}
}
