package ai

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"
)

// pi v1.3.0 utils/event-stream.ts result() returns one shared Promise, so
// every caller waiting before completion receives the same final result.
func TestEventStreamResultWakesAllCallers(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			stream := NewEventStream(func(event int) bool { return terminal }, func(event int) int { return event })
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			results := make(chan error, 4)
			for range 4 {
				go func() {
					value, err := stream.Result(ctx)
					if err == nil && value != 7 {
						err = fmt.Errorf("result = %d; want 7", value)
					}
					results <- err
				}()
			}
			synctest.Wait()
			if terminal {
				stream.Push(7)
			} else {
				value := 7
				stream.End(&value)
			}
			for range 4 {
				if err := <-results; err != nil {
					t.Errorf("Result: %v", err)
				}
			}
		})
	}
}

// pi v1.3.0 utils/event-stream.ts invokes its callbacks synchronously, and
// they can call end() again. The first resolved result wins while the outer
// push still delivers its event, even when the predicate itself ends it.
func TestEventStreamCallbacksCanEndStream(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprint(terminal), func(t *testing.T) {
			var stream *EventStream[int, int]
			ended := 9
			stream = NewEventStream(func(event int) bool {
				if !terminal {
					stream.End(&ended)
				}
				return terminal
			}, func(event int) int {
				stream.End(&ended)
				return event
			})
			pushed := make(chan struct{})
			go func() { stream.Push(7); close(pushed) }()
			select {
			case <-pushed:
			case <-time.After(time.Second):
				t.Fatal("callback deadlocked while ending stream")
			}
			result, err := stream.Result(context.Background())
			if err != nil || result != ended {
				t.Fatalf("result=%d, error=%v; want 9", result, err)
			}
			events := stream.Events(context.Background())
			if len(events) != 1 || events[0] != 7 {
				t.Fatalf("events=%v; want [7]", events)
			}
		})
	}
}

// pi v1.3.0 utils/event-stream.ts publishes its terminal event synchronously.
// A consumer must not observe EOF between terminal admission and publishing
// its event while the result extractor runs without the stream lock.
func TestEventStreamDeliversTerminalDuringExtraction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		stream := NewEventStream(func(int) bool { return true }, func(event int) int {
			close(entered)
			<-release
			return event
		})
		go stream.Push(7)
		<-entered
		result := make(chan iteratorResult[int], 1)
		go func() {
			value, ok := stream.Next(context.Background())
			result <- iteratorResult[int]{value: value, done: !ok}
		}()
		synctest.Wait()
		select {
		case early := <-result:
			t.Fatalf("consumer returned before terminal event: %+v", early)
		default:
		}
		close(release)
		got := <-result
		if got.done || got.value != 7 {
			t.Fatalf("Next=%+v; want value 7", got)
		}
	})
}

// Port of packages/ai/test/event-stream.test.ts — regression tests for
// upstream issue #9055.

func TestEventStreamDrainsBufferedInOrderAndIgnoresAfterCompletion(t *testing.T) {
	stream := NewEventStream(
		func(event int) bool { return event == 3 },
		func(event int) int { return event },
	)
	stream.Push(1)
	stream.Push(2)
	stream.Push(3)
	stream.Push(4)

	result, err := stream.Result(context.Background())
	if err != nil || result != 3 {
		t.Fatalf("Result() = %v, %v; want 3", result, err)
	}

	events := stream.Events(context.Background())
	if len(events) != 3 || events[0] != 1 || events[1] != 2 || events[2] != 3 {
		t.Fatalf("events = %v; want [1 2 3]", events)
	}
}

func TestEventStreamPreservesOrderWhenEventsArriveAfterDrainingStarts(t *testing.T) {
	stream := NewEventStream(
		func(event int) bool { return false },
		func(event int) int { return event },
	)
	stream.Push(1)
	stream.Push(2)
	ctx := context.Background()

	if ev, ok := stream.Next(ctx); !ok || ev != 1 {
		t.Fatalf("first Next = %v, %v; want 1", ev, ok)
	}
	stream.Push(3)
	if ev, ok := stream.Next(ctx); !ok || ev != 2 {
		t.Fatalf("second Next = %v, %v; want 2", ev, ok)
	}
	if ev, ok := stream.Next(ctx); !ok || ev != 3 {
		t.Fatalf("third Next = %v, %v; want 3", ev, ok)
	}
	three := 3
	stream.End(&three)
	if ev, ok := stream.Next(ctx); ok {
		t.Fatalf("Next after End = %v, %v; want done", ev, ok)
	}
}

// waitRegistered polls until n consumers are registered as waiters.
func waitRegistered(t *testing.T, stream *EventStream[int, int], n int) {
	t.Helper()
	for deadline := time.Now().Add(time.Second); ; {
		stream.mu.Lock()
		nWait := len(stream.waiting)
		stream.mu.Unlock()
		if nWait >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("consumer %d did not register", n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestEventStreamDeliversToWaitingConsumersInRegistrationOrder(t *testing.T) {
	stream := NewEventStream(
		func(event int) bool { return false },
		func(event int) int { return event },
	)

	start1 := make(chan struct{})
	start2 := make(chan struct{})
	first := make(chan int, 1)
	second := make(chan int, 1)
	go func() {
		<-start1
		ev, ok := stream.Next(context.Background())
		if ok {
			first <- ev
		}
	}()
	go func() {
		<-start2
		ev, ok := stream.Next(context.Background())
		if ok {
			second <- ev
		}
	}()

	// Wake the first consumer and let it register before starting the
	// second, so registration order is deterministic.
	close(start1)
	waitRegistered(t, stream, 1)
	close(start2)
	waitRegistered(t, stream, 2)
	stream.Push(1)
	stream.Push(2)

	if got := <-first; got != 1 {
		t.Fatalf("first consumer got %d; want 1", got)
	}
	if got := <-second; got != 2 {
		t.Fatalf("second consumer got %d; want 2", got)
	}
}

func TestEventStreamDrainsAfterEndAndResolvesExplicitResult(t *testing.T) {
	stream := NewEventStream(
		func(event int) bool { return false },
		func(event int) string { return string(rune('0' + event)) },
	)
	stream.Push(1)
	stream.Push(2)
	complete := "complete"
	stream.End(&complete)

	result, err := stream.Result(context.Background())
	if err != nil || result != "complete" {
		t.Fatalf("Result() = %q, %v; want complete", result, err)
	}

	got := stream.Events(context.Background())
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("events = %v; want [1 2]", got)
	}
}

func TestEventStreamWakesAllWaitingConsumersWhenEndedWithoutResult(t *testing.T) {
	stream := NewEventStream(
		func(event int) bool { return false },
		func(event int) int { return event },
	)

	first := make(chan bool, 1)
	second := make(chan bool, 1)
	go func() { _, ok := stream.Next(context.Background()); first <- ok }()
	waitRegistered(t, stream, 1)
	go func() { _, ok := stream.Next(context.Background()); second <- ok }()
	waitRegistered(t, stream, 2)
	stream.End(nil)

	if ok := <-first; ok {
		t.Fatal("first consumer should be done")
	}
	if ok := <-second; ok {
		t.Fatal("second consumer should be done")
	}
}

func TestAssistantMessageEventStreamResult(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	msg := &AssistantMessage{StopReason: StopStop}
	stream.Push(AssistantMessageEvent{Type: EventStart, Partial: msg})
	stream.Push(AssistantMessageEvent{Type: EventDone, Reason: StopStop, Message: msg})
	// Pushes after terminal events are ignored.
	stream.Push(AssistantMessageEvent{Type: EventTextDelta, Delta: "late"})

	result, err := stream.Result(context.Background())
	if err != nil || result != msg {
		t.Fatalf("Result() = %v, %v; want message", result, err)
	}
	events := stream.Events(context.Background())
	if len(events) != 2 {
		t.Fatalf("events = %d; want 2 (start + done)", len(events))
	}
}

func TestAssistantMessageEventStreamErrorResult(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	errMsg := &AssistantMessage{StopReason: StopError}
	stream.Push(AssistantMessageEvent{Type: EventError, Reason: StopError, Error: errMsg})

	result, err := stream.Result(context.Background())
	if err != nil || result != errMsg {
		t.Fatalf("Result() = %v, %v; want error message", result, err)
	}
}

// Reference: pi v1.4.0 utils/event-stream.ts AssistantMessageEventStream#time:
// Math.max(0, Math.round(performance.now() - startedAt)). The duration is part
// of the persisted assistant message, so a truncated millisecond would change
// the transcript bytes.
func TestAssistantStreamDurationRoundsToNearestMillisecond(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	stream.startedAt = time.Now().Add(-time.Hour).UnixMilli()
	stream.startedAtMonotonic = time.Now().Add(-1500 * time.Microsecond)
	message := &AssistantMessage{StopReason: StopStop, Timestamp: time.Now().UnixMilli()}
	stream.End(&message)
	if message.DurationMs == nil || *message.DurationMs != 2 {
		t.Fatalf("durationMs = %v, want 2", *message.DurationMs)
	}

	stream = NewAssistantMessageEventStream()
	stream.startedAt = time.Now().Add(-time.Hour).UnixMilli()
	stream.startedAtMonotonic = time.Now().Add(-1499 * time.Microsecond)
	message = &AssistantMessage{StopReason: StopStop, Timestamp: time.Now().UnixMilli()}
	stream.End(&message)
	if message.DurationMs == nil || *message.DurationMs != 1 {
		t.Fatalf("durationMs = %v, want 1", *message.DurationMs)
	}
}
