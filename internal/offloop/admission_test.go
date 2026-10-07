package offloop

import (
	"context"
	"reflect"
	"testing"
	"testing/synctest"
)

// D201: replace redundant pending work in O(1), without changing a running
// task. A fixed key leaves at most one pending successor for that domain.
func TestLatestOptionalTaskReplacesPendingWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := NewGroup()
		q := g.OptionalQueue()
		latest, ok := any(q).(interface {
			GoLatestContext(string, func(context.Context)) bool
		})
		if !ok {
			t.Fatal("optional queue cannot replace redundant pending work")
		}
		release := make(chan struct{})
		defer func() { close(release); g.StopAll(); q.Flush() }()
		var order []int
		q.GoContext(func(context.Context) { <-release; order = append(order, 0) })
		synctest.Wait()
		for i := 1; i <= 64; i++ {
			if !latest.GoLatestContext("theme", func(context.Context) { order = append(order, i) }) {
				t.Fatal("latest work rejected")
			}
		}
		queued, running := q.Backlog()
		if queued != 1 || running != 1 {
			t.Fatalf("latest backlog = (%d,%d), want (1,1)", queued, running)
		}
		release <- struct{}{}
		q.Flush()
		if !reflect.DeepEqual(order, []int{0, 64}) {
			t.Fatalf("latest delivery = %v, want [0 64]", order)
		}
		g.StopAll()
		if latest.GoLatestContext("theme", func(context.Context) {}) {
			t.Fatal("stopped queue accepted latest work")
		}
	})
}

// D201: optional admission rejects excess work without blocking submitters,
// while accepted work remains FIFO and capacity is reusable after completion.
func TestBoundedOptionalAdmissionDuringBlockedWorker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := NewGroup()
		q := g.OptionalQueue()
		admission, ok := any(q).(interface {
			TryGoContext(int, func(context.Context)) bool
		})
		if !ok {
			t.Fatal("optional queue has no bounded admission")
		}
		release := make(chan struct{})
		defer func() { close(release); g.StopAll(); q.Flush() }()
		var order []int
		if !admission.TryGoContext(4, func(context.Context) { <-release; order = append(order, 0) }) {
			t.Fatal("first task rejected")
		}
		synctest.Wait()
		accepted := 1
		for i := 1; i < 65; i++ {
			if admission.TryGoContext(4, func(context.Context) { order = append(order, i) }) {
				accepted++
			}
		}
		queued, running := q.Backlog()
		t.Logf("64 extra submissions: accepted=%d queued=%d running=%d", accepted, queued, running)
		if accepted != 4 || queued != 3 || running != 1 {
			t.Fatalf("admission = %d, backlog = (%d,%d), want 4 and (3,1)", accepted, queued, running)
		}
		release <- struct{}{}
		q.Flush()
		if !reflect.DeepEqual(order, []int{0, 1, 2, 3}) {
			t.Fatalf("accepted FIFO = %v", order)
		}
		if !admission.TryGoContext(4, func(context.Context) { order = append(order, 4) }) {
			t.Fatal("capacity not reclaimed")
		}
		q.Flush()
		g.StopAll()
		if admission.TryGoContext(4, func(context.Context) { t.Error("late task ran") }) {
			t.Fatal("stopped queue accepted work")
		}
	})
}
