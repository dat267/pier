package ai

import (
	"context"
	"testing"
)

// pi v1.4.0 (4a080252), utils/event-stream.ts and types.ts retain the live
// partial helper. D285 adds an opt-in owned handoff for the Go agent without
// changing Next's upstream identity/late-observation behavior.
func TestAssistantStreamForwardingPreservesProducerSnapshot(t *testing.T) {
	inner, outer := NewAssistantMessageEventStream(), NewAssistantMessageEventStream()
	partial := &AssistantMessage{Content: ContentList{TextContent{}}}
	inner.Push(AssistantMessageEvent{Type: EventTextStart, Partial: partial})
	event, _ := inner.Next(context.Background())
	partial.Content[0] = TextContent{Text: "later"}
	outer.Push(event)
	snapshot, ok := outer.NextSnapshot(context.Background())
	if !ok || snapshot.Partial.Content[0].(TextContent).Text != "" {
		t.Fatalf("forwarder recaptured a mutated live partial: %+v", snapshot.Partial)
	}
}

func TestAssistantStreamSnapshotDoesNotChangeLiveNext(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	partial := &AssistantMessage{Content: ContentList{TextContent{}}}
	stream.Push(AssistantMessageEvent{Type: EventTextStart, Partial: partial})
	partial.Content[0] = TextContent{Text: "later"}
	partial.Usage.Output = 2
	stream.Push(AssistantMessageEvent{Type: EventTextDelta, Partial: partial, Delta: "later"})
	partial.Content[0] = TextContent{Text: "latest"}
	partial.Usage.Output = 3

	snapshot, ok := stream.NextSnapshot(context.Background())
	if !ok || snapshot.Partial == partial || snapshot.Partial.Content[0].(TextContent).Text != "" || snapshot.Partial.Usage.Output != 0 {
		t.Fatalf("start snapshot = %+v", snapshot.Partial)
	}
	live, ok := stream.Next(context.Background())
	if !ok || live.Partial != partial || live.Partial.Content[0].(TextContent).Text != "latest" || live.Partial.Usage.Output != 3 {
		t.Fatalf("Next lost the shared live helper: %+v", live.Partial)
	}
}
