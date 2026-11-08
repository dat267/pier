package ai

import (
	"context"
	"strings"
	"testing"
	"time"
)

// streamLargeToolCall pushes a throttled tool-call stream: a 4 KiB argument
// in 16-byte deltas with the frame clock stopped, so every preview after the
// first 1 KiB is held back.
func streamLargeToolCall(t *testing.T) (*AssistantMessageEventStream, *streamingArguments, *AssistantMessage, string) {
	t.Helper()
	fakeStreamingJSONClock(t, 0)
	stream := NewAssistantMessageEventStream()
	output := &AssistantMessage{Content: ContentList{ToolCall{ID: "t", Name: "write", Arguments: []byte(`{}`)}}}
	args := &streamingArguments{}
	text := `{"path":"a.txt","content":"` + strings.Repeat("x", 4096)
	for i := 0; i < len(text); i += 16 {
		delta := text[i:min(i+16, len(text))]
		call := output.Content[0].(ToolCall)
		call.Arguments = args.Append(delta)
		output.Content[0] = call
		stream.Push(AssistantMessageEvent{Type: EventToolcallDelta, ContentIndex: 0, Delta: delta, Partial: output}.withArguments(args))
	}
	return stream, args, output, text
}

func drainDeltas(t *testing.T, stream *AssistantMessageEventStream, n int) AssistantMessageEvent {
	t.Helper()
	var last AssistantMessageEvent
	for range n {
		event, ok := stream.NextSnapshot(context.Background())
		if !ok || event.Type != EventToolcallDelta || event.Delta == "" {
			t.Fatalf("expected a provider toolcall_delta, got %+v ok=%v", event, ok)
		}
		last = event
	}
	return last
}

// D353: a stream that pauses after a throttled preview still shows every
// argument byte received: NextSnapshot waits a frame, then returns a
// toolcall_delta with an empty delta carrying the current preview.
func TestStreamingArgumentsPauseRefreshesThrottledPreview(t *testing.T) {
	stream, _, _, text := streamLargeToolCall(t)
	deltas := (len(text) + 15) / 16
	last := drainDeltas(t, stream, deltas)
	if got := string(last.Partial.Content[0].(ToolCall).Arguments); got == text+`"}` {
		t.Fatal("the last provider preview was not throttled; the test needs a held-back preview")
	}
	start := time.Now()
	refresh, ok := stream.NextSnapshot(context.Background())
	if !ok || refresh.Type != EventToolcallDelta || refresh.Delta != "" || refresh.ContentIndex != 0 {
		t.Fatalf("pause refresh = %+v ok=%v", refresh, ok)
	}
	if got, want := string(refresh.Partial.Content[0].(ToolCall).Arguments), text+`"}`; got != want {
		t.Fatalf("refreshed preview is %d bytes, want the %d bytes received", len(got), len(want))
	}
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("refresh took %v", waited)
	}
	// The refresh is current: the next wait blocks until the provider speaks.
	stream.Push(AssistantMessageEvent{Type: EventToolcallEnd, ContentIndex: 0, Partial: refresh.Partial})
	if event, _ := stream.NextSnapshot(context.Background()); event.Type != EventToolcallEnd {
		t.Fatalf("after the refresh got %s, want toolcall_end", event.Type)
	}
}

// Text appended after the last delivered event belongs to that text's own
// event, so the pause refresh stands aside and the consumer waits for it.
func TestStreamingArgumentsPauseRefreshWaitsForUndeliveredText(t *testing.T) {
	stream, args, output, text := streamLargeToolCall(t)
	drainDeltas(t, stream, (len(text)+15)/16)
	call := output.Content[0].(ToolCall)
	call.Arguments = args.Append(`yy`)
	output.Content[0] = call
	done := make(chan AssistantMessageEvent)
	go func() {
		event, _ := stream.NextSnapshot(context.Background())
		done <- event
	}()
	time.Sleep(3 * sjFrameNanosDuration())
	stream.Push(AssistantMessageEvent{Type: EventToolcallDelta, ContentIndex: 0, Delta: "yy", Partial: output})
	if event := <-done; event.Delta != "yy" {
		t.Fatalf("got %+v, want the provider's own delta", event)
	}
}

func sjFrameNanosDuration() time.Duration { return time.Duration(sjFrameNanos) }

// An event pushed as the timed wait gives up is returned, not lost.
func TestEventStreamNextWithinNeverLosesAnEvent(t *testing.T) {
	for range 200 {
		stream := NewEventStream[int, int](nil, nil)
		got := make(chan int, 1)
		go func() {
			v, ok, timedOut := stream.nextWithin(context.Background(), time.Microsecond)
			if timedOut {
				v, ok = stream.Next(context.Background())
			}
			if !ok {
				v = -1
			}
			got <- v
		}()
		stream.Push(7)
		if v := <-got; v != 7 {
			t.Fatalf("got %d, want the pushed event", v)
		}
	}
	stream := NewEventStream[int, int](nil, nil)
	if _, ok, timedOut := stream.nextWithin(context.Background(), time.Millisecond); ok || !timedOut {
		t.Fatalf("empty stream: ok=%v timedOut=%v", ok, timedOut)
	}
}
