package interactive

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// sessionEventQueue decouples the session-event producers (the agent run
// goroutine, background workers, and any other emitter) from the single UI
// consumer (the run loop in interactivemode_run.go). Producers only enqueue;
// they never touch UI state or UI locks.
//
// Two channels carry the two classes of events:
//
//   - partial: streaming updates that supersede each other (assistant text
//     deltas, running tool/bash output). Bounded with an explicit drop-oldest
//     policy so a fast token stream can neither back-pressure the agent nor
//     starve input; the terminal event of each stream carries the final state,
//     so dropping intermediate partials is lossless in effect.
//   - lossless: everything else (message start/end, tool end, agent settled,
//     compaction, retries, ...). Never dropped: the send blocks until the loop
//     has room or the queue closes.
//
// Sizing: the loop is the only consumer and the producers are the agent run
// goroutine plus background workers that emit synchronously from their own
// goroutine. Both channels are buffered with room for every producer to have a
// message in flight, plus slack for the synchronous bursts a loop-initiated
// command produces (compaction start/end and their retry notices).
const (
	sessionEventLosslessCapacity = 256
	sessionEventPartialCapacity  = 8
)

// inputQueueCapacity sizes the user-submission channel: enough for a turn's
// worth of queued submissions without blocking the TUI submit handler.
const inputQueueCapacity = 64

// loopInputCapacity sizes the terminal-sequence channel. One producer (the
// stdin reader goroutine) writes it; the buffer absorbs a paste burst before
// the loop drains.
const loopInputCapacity = 256

// D196: split channels do not preserve upstream's emission order. Stamp
// assistant events at the producer so a delayed partial cannot update a later
// message, even when its start overtakes the partial in the lossless channel.
// The envelope is UI-local and is never persisted or sent to providers.
type queuedSessionEvent struct {
	*coding.SessionEvent
	messageGeneration uint64
}

type sessionEventQueue struct {
	messageGeneration atomic.Uint64
	lossless          chan queuedSessionEvent
	partial           chan queuedSessionEvent
	closed            chan struct{}

	// ctx is the run context; producers also unblock on its cancellation
	// (stage 4 gap: every send selects on ctx.Done() as well as Close).
	ctx atomic.Pointer[context.Context]

	once sync.Once
}

func newSessionEventQueue() *sessionEventQueue {
	return &sessionEventQueue{
		lossless: make(chan queuedSessionEvent, sessionEventLosslessCapacity),
		partial:  make(chan queuedSessionEvent, sessionEventPartialCapacity),
		closed:   make(chan struct{}),
	}
}

// SetContext installs the run context producers select on.
func (q *sessionEventQueue) SetContext(ctx context.Context) {
	if q == nil {
		return
	}
	q.ctx.Store(&ctx)
}

// done returns a channel closed when the queue shuts down or the run context
// is cancelled.
func (q *sessionEventQueue) done() <-chan struct{} {
	if ctx := q.ctx.Load(); ctx != nil {
		return (*ctx).Done()
	}
	return q.closed
}

// Events/Lossless/Partials expose the consumer sides to the run loop.
func (q *sessionEventQueue) Events() <-chan queuedSessionEvent   { return q.lossless }
func (q *sessionEventQueue) Partials() <-chan queuedSessionEvent { return q.partial }

// enqueue is the producer entry point (the session subscription callback). It
// never blocks on a full partial channel and never calls into the UI.
func (q *sessionEventQueue) enqueue(event *coding.SessionEvent) {
	if q == nil || event == nil {
		return
	}
	generation := q.messageGeneration.Load()
	if event.Type == coding.SessionMessageStart && event.Agent != nil {
		if _, ok := event.Agent.Message.(*ai.AssistantMessage); ok {
			generation = q.messageGeneration.Add(1)
		}
	}
	queued := queuedSessionEvent{SessionEvent: event, messageGeneration: generation}
	if isPartialSessionEvent(event.Type) {
		select {
		case q.partial <- queued:
		default:
			// Latest wins: drop the oldest pending partial, then retry once.
			select {
			case <-q.partial:
			default:
			}
			select {
			case q.partial <- queued:
			default:
			}
		}
		return
	}
	select {
	case q.lossless <- queued:
	case <-q.closed:
		// Shutting down: the consumer is gone, so unblock the producer.
	case <-q.done():
		// The run context was cancelled (shutdown without Close).
	}
}

// Close releases producers blocked on a full lossless channel. It is
// idempotent and is called once the loop has stopped consuming.
func (q *sessionEventQueue) Close() {
	if q == nil {
		return
	}
	q.once.Do(func() { close(q.closed) })
}

// isPartialSessionEvent reports whether an event is a superseded streaming
// update (coalescable) rather than a terminal state change (lossless).
func isPartialSessionEvent(eventType coding.SessionEventType) bool {
	switch eventType {
	case coding.SessionMessageUpdate,
		coding.SessionToolExecutionUpdate,
		coding.SessionBashExecutionUpdate:
		return true
	}
	return false
}
