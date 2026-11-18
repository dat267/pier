package offloop

import (
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// The queue is the uniform mechanism for running work off the UI goroutine:
// one background goroutine per queue, strict submission order, optional
// keyed coalescing, and a Flush for tests and shutdown. Completions marshal
// back onto the loop through the caller's existing UI.Post pattern; the queue
// governs only the off-loop side.

func TestQueueRunsTasksInSubmissionOrder(t *testing.T) {
	q := New()
	t.Cleanup(q.Stop)
	var mu sync.Mutex
	var order []int
	for i := range 50 {
		q.Go(func() {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, i)
		})
	}
	q.Flush()
	mu.Lock()
	defer mu.Unlock()
	for i, got := range order {
		if got != i {
			t.Fatalf("order[%d] = %d", i, got)
		}
	}
}

func TestQueueCoalescesSameKeyWhileTaskRuns(t *testing.T) {
	q := New()
	t.Cleanup(q.Stop)
	release := make(chan struct{})
	var ran atomic.Int32
	q.GoCoalesced("paste", func() {
		<-release
		ran.Add(1)
	})
	// While the first task runs, same-key submissions are dropped and
	// different-key submissions queue behind it.
	q.GoCoalesced("paste", func() { ran.Add(100) })
	q.GoCoalesced("other", func() { ran.Add(10) })
	close(release)
	q.Flush()
	if got := ran.Load(); got != 11 {
		t.Fatalf("ran = %d, want 11 (paste dropped, other ran)", got)
	}
	// The key is free again once the task finished.
	q.GoCoalesced("paste", func() { ran.Add(1) })
	q.Flush()
	if got := ran.Load(); got != 12 {
		t.Fatalf("ran = %d, want 12 (key reusable)", got)
	}
}

func TestQueueFlushWaitsForRunningTask(t *testing.T) {
	q := New()
	t.Cleanup(q.Stop)
	var done atomic.Bool
	q.Go(func() {
		time.Sleep(20 * time.Millisecond)
		done.Store(true)
	})
	q.Flush()
	if !done.Load() {
		t.Fatal("Flush returned before the task finished")
	}
}

func TestQueueStopRejectsNewTasksButDrainsQueued(t *testing.T) {
	q := New()
	t.Cleanup(q.Stop)
	var ran atomic.Int32
	q.Go(func() { ran.Add(1) })
	q.Stop()
	q.Go(func() { ran.Add(1) })
	if got := ran.Load(); got != 1 {
		t.Fatalf("ran = %d, want 1 (queued task drained, later task dropped)", got)
	}
	q.Flush() // Flush after Stop returns immediately
}

func TestQueueConcurrentSubmitsKeepOrderPerSubmission(t *testing.T) {
	q := New()
	t.Cleanup(q.Stop)
	const n = 200
	var counter atomic.Int64
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			q.Go(func() { counter.Add(1) })
		})
	}
	wg.Wait()
	q.Flush()
	if got := counter.Load(); got != n {
		t.Fatalf("ran = %d, want %d", got, n)
	}
}

// The backlog is measured through the queue while a channel-gated worker is
// blocked. No wall-clock sleeps or scheduling assumptions are needed.
func TestQueueBacklogDuringBlockedShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := New()
		release := make(chan struct{})
		defer q.Stop()
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		backlog := q.Backlog
		var order []int
		q.Go(func() { <-release; order = append(order, 0) })
		synctest.Wait()
		queued, running := backlog()
		if queued != 0 || running != 1 {
			t.Fatalf("initial backlog = (%d, %d), want (0, 1)", queued, running)
		}
		for i := 1; i <= 64; i++ {
			q.Go(func() { order = append(order, i) })
		}
		queued, running = backlog()
		if queued != 64 || running != 1 {
			t.Fatalf("blocked backlog = (%d, %d), want (64, 1)", queued, running)
		}
		t.Logf("blocked worker retained %d queued tasks and %d running task", queued, running)
		stopped := make(chan struct{})
		go func() { q.Stop(); close(stopped) }()
		synctest.Wait()
		select {
		case <-stopped:
			t.Fatal("Stop returned before the blocked task was released")
		default:
		}
		q.Go(func() { order = append(order, -1) })
		queued, running = backlog()
		if queued != 64 || running != 1 {
			t.Fatalf("shutdown accepted another task: backlog = (%d, %d)", queued, running)
		}
		unblock()
		synctest.Wait()
		select {
		case <-stopped:
		default:
			t.Fatal("Stop did not complete after releasing the worker")
		}
		queued, running = backlog()
		if queued != 0 || running != 0 {
			t.Fatalf("drained backlog = (%d, %d), want (0, 0)", queued, running)
		}
		want := make([]int, 65)
		for i := range want {
			want[i] = i
		}
		if !reflect.DeepEqual(order, want) {
			t.Fatalf("drained order = %v, want %v", order, want)
		}
	})
}

// Go-only D201 contract: unlike pi v1.4.0 interactive-mode.ts, Go queues must
// track overlapping workers explicitly. A completed key must become reusable,
// including the empty string, which GoCoalesced does not reserve or reject.
func TestCoalescedEmptyKeyBecomesReusable(t *testing.T) {
	q := New()
	t.Cleanup(q.Stop)
	var calls int
	q.GoCoalesced("", func() { calls++ })
	q.Flush()
	q.GoCoalesced("", func() { calls++ })
	q.Flush()
	if calls != 2 {
		t.Fatalf("completed empty-key task left key reserved: calls=%d, want 2", calls)
	}
}
