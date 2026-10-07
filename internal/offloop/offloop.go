// Package offloop provides the uniform mechanism for running work off the UI
// goroutine: one background goroutine per queue, strict submission order,
// optional keyed coalescing, and a Flush for tests and shutdown.
//
// The invariant it serves: nothing on the main event loop may block — no
// syscalls, file or network I/O, subprocesses, lock retries, or sleeps. Work
// that would block is handed to a queue; results marshal back onto the loop
// through the caller's existing UI.Post / runOnUI pattern. The queue governs
// only the off-loop side and never touches the UI itself.
//
// Domains get their own queue: a 5s clipboard hang must not delay a settings
// write. Ordering is guaranteed per queue, not across queues. Flush must not
// be called from a task of the same queue. A Group bundles the queues of one
// composition root: FlushAll explicitly drains all queues; StopAll cancels
// optional work and drains mandatory queues before returning.
package offloop

import (
	"context"
	"sync"
)

type task struct {
	key    string // coalesce key, "" when not coalesced
	run    func()
	latest bool
}

type latestTask struct {
	run func(context.Context)
}

// Queue serializes tasks onto one background goroutine.
type Queue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	tasks    []task
	inFlight map[string]bool // coalesce keys with a queued or running task
	active   int             // accepted tasks not yet finished
	started  bool
	stopped  bool
	ctx      context.Context
	cancel   context.CancelFunc     // non-nil only for optional queues (D199)
	latest   map[string]*latestTask // pending replacements only, guarded by mu
}

// New builds an idle queue. The worker goroutine starts with the first task
// and exits after Stop once the queue drains.
func New() *Queue {
	q := &Queue{inFlight: map[string]bool{}, ctx: context.Background()}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Go submits a task to run in submission order. It never blocks. Tasks
// submitted after Stop are dropped.
func (q *Queue) Go(run func()) { q.enqueue(task{run: run}) }

// GoContext submits work with the queue's lifetime context. Mandatory queues
// use Background; optional queues cancel it on Stop. A blocking operation must
// honor this context itself; Stop cannot forcibly terminate a goroutine.
func (q *Queue) GoContext(run func(context.Context)) {
	q.Go(func() { run(q.ctx) })
}

// TryGoContext admits at most maxActive queued plus running tasks. Rejection
// never waits for capacity. D201: use only for best-effort domains, not saves.
func (q *Queue) TryGoContext(maxActive int, run func(context.Context)) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped || maxActive <= 0 || q.active >= maxActive {
		return false
	}
	q.enqueueLocked(task{run: func() { run(q.ctx) }})
	return true
}

// GoLatestContext keeps at most one pending task per key, replacing its body
// in O(1). A running task is never replaced. D201: callers must use a bounded
// key set and only best-effort work; never use this for persistence.
func (q *Queue) GoLatestContext(key string, run func(context.Context)) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped {
		return false
	}
	if pending := q.latest[key]; pending != nil {
		pending.run = run
		return true
	}
	if q.latest == nil {
		q.latest = make(map[string]*latestTask)
	}
	pending := &latestTask{run: run}
	q.latest[key] = pending
	q.enqueueLocked(task{key: key, latest: true, run: func() { pending.run(q.ctx) }})
	return true
}

// GoCoalesced submits a task unless a task with the same key is queued or
// running, in which case the submission is dropped. This reproduces the
// drop-overlapping-work idiom (a paste while a paste read is still running)
// without a hand-rolled flag. The key frees up when the task finishes.
func (q *Queue) GoCoalesced(key string, run func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped || q.inFlight[key] {
		return
	}
	q.inFlight[key] = true
	q.enqueueLocked(task{key: key, run: run})
}

// GoCoalescedContext is GoCoalesced with the queue's lifetime context.
func (q *Queue) GoCoalescedContext(key string, run func(context.Context)) {
	q.GoCoalesced(key, func() { run(q.ctx) })
}

// Backlog snapshots accepted work: queued tasks and the running task (0 or 1).
// It does not include dropped coalesced submissions or impose a queue limit.
func (q *Queue) Backlog() (queued, running int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	queued = len(q.tasks)
	return queued, q.active - queued
}

// Flush blocks until running and still-queued tasks finish. Optional tasks
// discarded by Stop do not need to run, but Flush still waits for a canceled
// running worker. Do not use Flush as an optional-work shutdown wait.
func (q *Queue) Flush() {
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.active > 0 {
		q.cond.Wait()
	}
}

// Stop rejects future tasks. Mandatory queues drain accepted work; optional
// queues cancel running work, discard queued tasks and return without waiting
// for a worker that ignores cancellation (D199). Safe to call more than once.
func (q *Queue) Stop() {
	if !q.stopAccepting() {
		q.Flush()
	}
}

// stopAccepting begins shutdown without waiting, returning whether the queue
// is optional. Group closes every queue before waiting for mandatory drains.
func (q *Queue) stopAccepting() bool {
	q.mu.Lock()
	q.stopped = true
	if q.cancel != nil {
		q.active -= len(q.tasks)
		q.tasks = nil
		q.inFlight = nil
		q.latest = nil
	}
	q.cond.Broadcast()
	q.mu.Unlock()
	if q.cancel != nil {
		q.cancel()
		return true
	}
	return false
}

func (q *Queue) enqueue(t task) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped {
		return
	}
	q.enqueueLocked(t)
}

func (q *Queue) enqueueLocked(t task) {
	q.tasks = append(q.tasks, t)
	q.active++
	if !q.started {
		q.started = true
		go q.loop()
	}
	q.cond.Signal()
}

func (q *Queue) loop() {
	for {
		q.mu.Lock()
		for len(q.tasks) == 0 && !q.stopped {
			q.cond.Wait()
		}
		if len(q.tasks) == 0 { // stopped and drained
			q.mu.Unlock()
			return
		}
		t := q.tasks[0]
		q.tasks = q.tasks[1:]
		if t.latest {
			delete(q.latest, t.key)
		}
		q.mu.Unlock()

		t.run()

		q.mu.Lock()
		if t.key != "" && !t.latest {
			delete(q.inFlight, t.key)
		}
		q.active--
		if q.active == 0 {
			q.cond.Broadcast()
		}
		q.mu.Unlock()
	}
}
