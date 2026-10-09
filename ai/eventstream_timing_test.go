package ai

import (
	"testing"
	"time"
)

// The final message of a response the stream saw start is timed (upstream #10549,
// utils/event-stream.ts).
func TestAssistantStreamTimesTheFinalMessage(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	message := &AssistantMessage{Timestamp: time.Now().UnixMilli()}
	stream.Push(AssistantMessageEvent{Type: EventDone, Reason: StopStop, Message: message})
	if message.DurationMs == nil {
		t.Fatal("a done event was not timed")
	}
	if *message.DurationMs < 0 {
		t.Fatalf("durationMs = %d", *message.DurationMs)
	}

	// An error event is timed the same way.
	failed := NewAssistantMessageEventStream()
	failure := &AssistantMessage{Timestamp: time.Now().UnixMilli(), StopReason: StopError}
	failed.Push(AssistantMessageEvent{Type: EventError, Reason: StopError, Error: failure})
	if failure.DurationMs == nil {
		t.Fatal("an error event was not timed")
	}

	// A result passed straight to End is timed too.
	settled := NewAssistantMessageEventStream()
	result := &AssistantMessage{Timestamp: time.Now().UnixMilli()}
	settled.End(&result)
	if result.DurationMs == nil {
		t.Fatal("a result passed to End was not timed")
	}
}

// A message that already carries a duration keeps it, and one that started before the stream
// is never timed: that is how a forwarded or deferred result stays untimed.
func TestAssistantStreamLeavesForwardedMessagesAlone(t *testing.T) {
	stream := NewAssistantMessageEventStream()

	existing := int64(1234)
	already := &AssistantMessage{Timestamp: time.Now().UnixMilli(), DurationMs: &existing}
	stream.Push(AssistantMessageEvent{Type: EventDone, Reason: StopStop, Message: already})
	if already.DurationMs == nil || *already.DurationMs != existing {
		t.Fatalf("an existing duration was overwritten: %v", already.DurationMs)
	}

	forwarded := &AssistantMessage{Timestamp: stream.startedAt - 1000}
	stream.Push(AssistantMessageEvent{Type: EventDone, Reason: StopStop, Message: forwarded})
	if forwarded.DurationMs != nil {
		t.Fatalf("a message that started before the stream was timed: %d", *forwarded.DurationMs)
	}

	// A second terminal event after the stream is done does not re-time anything.
	late := &AssistantMessage{Timestamp: time.Now().UnixMilli()}
	stream.Push(AssistantMessageEvent{Type: EventDone, Reason: StopStop, Message: late})
	if late.DurationMs != nil {
		t.Fatalf("a message after the stream settled was timed: %d", *late.DurationMs)
	}
}
