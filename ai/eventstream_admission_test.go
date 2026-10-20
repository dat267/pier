package ai

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
)

// Reference: pi v1.3.0 utils/event-stream.ts synchronous completion closes
// admission before another producer can complete. Never wait for both predicates:
// that would deadlock an implementation that serializes admission.
func TestConcurrentTerminalPushAdmitsOneEvent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		s := NewEventStream(func(v int) bool {
			if v == 1 {
				close(entered)
				<-release
			}
			return true
		}, func(v int) int { return v })
		var wg sync.WaitGroup
		wg.Go(func() { s.Push(1) })
		<-entered
		wg.Go(func() { s.Push(2) })
		synctest.Wait()
		close(release)
		wg.Wait()
		events := s.Events(context.Background())
		if len(events) != 1 {
			t.Fatalf("events=%v; want one terminal event", events)
		}
		result, err := s.Result(context.Background())
		if err != nil || result != events[0] {
			t.Fatalf("result=%d error=%v events=%v", result, err, events)
		}
	})
}
