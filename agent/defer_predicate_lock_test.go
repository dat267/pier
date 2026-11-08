package agent

import (
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// The defer predicate is caller code. ARCHITECTURE.md's lock inventory states
// the rule for caller-supplied predicates ("run outside the mutex and may
// reenter"): a predicate that touches the queue it decides on must not
// deadlock, and neither queue read may hold the mutex while it runs.
//
// A self-lock cannot be detected in-band, so the probe runs each read on its
// own goroutine and fails on a watchdog timeout instead of hanging the suite.
func TestDeferPredicateRunsOutsideTheQueueLock(t *testing.T) {
	newAgent := func(t *testing.T, defer_ func(a *Agent) func(ai.Message) bool) *Agent {
		t.Helper()
		a, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn()})
		if err != nil {
			t.Fatal(err)
		}
		a.DeferFollowUp = defer_(a)
		a.FollowUp(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "queued"}})
		return a
	}
	runWithoutDeadlock := func(t *testing.T, name string, read func(*Agent) bool) {
		t.Helper()
		a := newAgent(t, func(a *Agent) func(ai.Message) bool {
			return func(ai.Message) bool {
				// Caller code that touches the same queue: HasQueuedMessages
				// takes the queue mutex the predicate used to run under.
				_ = a.HasQueuedMessages()
				return false
			}
		})
		done := make(chan bool, 1)
		go func() { done <- read(a) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the defer predicate deadlocked on the queue lock", name)
		}
	}
	t.Run("hasDeliverable", func(t *testing.T) {
		runWithoutDeadlock(t, "HasDeliverableMessages", func(a *Agent) bool { return a.HasDeliverableMessages() })
	})
	t.Run("drain", func(t *testing.T) {
		runWithoutDeadlock(t, "takeFollowUps", func(a *Agent) bool { return len(a.takeFollowUps()) > 0 })
	})
}

// The predicate still decides delivery: a held message stays queued in order,
// and the next drain, with a predicate that releases it, returns it.
func TestDeferPredicateKeepsHeldMessagesInOrder(t *testing.T) {
	a, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn()})
	if err != nil {
		t.Fatal(err)
	}
	held := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "held"}}
	delivered := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "delivered"}}
	a.SetFollowUpMode(QueueModeAll)
	a.FollowUp(held)
	a.FollowUp(delivered)

	a.DeferFollowUp = func(message ai.Message) bool { return message == ai.Message(held) }
	if got := a.takeFollowUps(); len(got) != 1 || got[0] != ai.Message(delivered) {
		t.Fatalf("drain = %v, want only the released message", got)
	}
	if got := a.takeFollowUps(); len(got) != 0 {
		t.Fatalf("second drain = %v, want nothing (the held message stays queued)", got)
	}
	a.DeferFollowUp = func(ai.Message) bool { return false }
	if got := a.takeFollowUps(); len(got) != 1 || got[0] != ai.Message(held) {
		t.Fatalf("released drain = %v, want the held message", got)
	}
}
