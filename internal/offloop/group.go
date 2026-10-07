package offloop

import (
	"context"
	"sync"
)

// Group owns the queues of one composition root. FlushAll explicitly waits for
// every queue; StopAll cancels optional work and drains mandatory saves. Workers
// stay independent and ordering is per queue, not across the group. Group exists
// because queues created in several places were once torn down by hand, and one
// was forgotten.
type Group struct {
	mu      sync.Mutex
	queues  []*Queue
	stopped bool
}

// NewGroup builds an empty group.
func NewGroup() *Group { return &Group{} }

// Queue creates a queue and registers it with the group.
func (g *Group) Queue() *Queue {
	q := New()
	g.Add(q)
	return q
}

// OptionalQueue registers best-effort work (D199). StopAll cancels its context,
// discards waiting tasks and does not wait for its running worker. Session and
// settings writes must use Queue, never OptionalQueue.
func (g *Group) OptionalQueue() *Queue {
	q := New()
	q.ctx, q.cancel = context.WithCancel(context.Background())
	g.Add(q)
	return q
}

// Add registers an existing queue. A queue added after StopAll is stopped
// immediately, so a late registration cannot leak a worker.
func (g *Group) Add(q *Queue) {
	g.mu.Lock()
	stopped := g.stopped
	if !stopped {
		g.queues = append(g.queues, q)
	}
	g.mu.Unlock()
	if stopped {
		q.Stop()
	}
}

// FlushAll blocks until every registered queue has drained. The queue list is
// snapshotted under the lock and the waits run outside it, so a slow queue
// cannot block a concurrent FlushAll or StopAll.
func (g *Group) FlushAll() {
	for _, q := range g.snapshot() {
		q.Flush()
	}
}

// StopAll rejects new work on every queue first, cancelling optional work
// before waiting for mandatory saves. Optional workers may finish later but
// must discard canceled results. Safe to call more than once.
func (g *Group) StopAll() {
	g.mu.Lock()
	g.stopped = true
	queues := g.queues
	g.mu.Unlock()
	for _, q := range queues {
		q.stopAccepting()
	}
	for _, q := range queues {
		if q.cancel == nil {
			q.Flush()
		}
	}
}

func (g *Group) snapshot() []*Queue {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]*Queue(nil), g.queues...)
}
