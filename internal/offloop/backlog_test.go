package offloop

import (
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
)

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
