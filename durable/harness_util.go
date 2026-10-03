package durable

import (
	"context"
	"errors"
	"sync"
)

// Port of harness/util.ts.

// Waiters holds pending waits by key. Each settles once, through Resolve,
// RejectAll, or cancellation of its context.
type Waiters[K comparable, T any] struct {
	mu   sync.Mutex
	sets map[K]map[*waiter[T]]struct{}
}

type waiter[T any] struct {
	done  chan T
	value T
	err   error
}

// Add waits for the key until Resolve, RejectAll or context cancellation.
func (w *Waiters[K, T]) Add(key K, ctx context.Context) (T, error) {
	w.mu.Lock()
	if w.sets == nil {
		w.sets = map[K]map[*waiter[T]]struct{}{}
	}
	pending := &waiter[T]{done: make(chan T, 1)}
	set := w.sets[key]
	if set == nil {
		set = map[*waiter[T]]struct{}{}
		w.sets[key] = set
	}
	set[pending] = struct{}{}
	w.mu.Unlock()

	if ctx != nil && ctx.Done() != nil {
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			select {
			case <-ctx.Done():
				w.mu.Lock()
				delete(set, pending)
				if len(set) == 0 {
					if existing, ok := w.sets[key]; ok && len(existing) == 0 {
						delete(w.sets, key)
					}
				}
				w.mu.Unlock()
				pending.err = ctx.Err()
				pending.done <- *new(T)
			case <-stop:
			}
		}()
	}
	value := <-pending.done
	if pending.err != nil {
		var zero T
		return zero, pending.err
	}
	return value, nil
}

// Keys lists the keys with pending waits.
func (w *Waiters[K, T]) Keys() []K {
	w.mu.Lock()
	defer w.mu.Unlock()
	keys := make([]K, 0, len(w.sets))
	for key := range w.sets {
		keys = append(keys, key)
	}
	return keys
}

// Resolve settles every wait on one key.
func (w *Waiters[K, T]) Resolve(key K, value T) {
	w.mu.Lock()
	set := w.sets[key]
	delete(w.sets, key)
	w.mu.Unlock()
	for pending := range set {
		pending.value = value
		pending.done <- value
	}
}

// RejectAll settles every wait with an error.
func (w *Waiters[K, T]) RejectAll(err error) {
	w.mu.Lock()
	sets := w.sets
	w.sets = map[K]map[*waiter[T]]struct{}{}
	w.mu.Unlock()
	for _, set := range sets {
		for pending := range set {
			pending.err = err
			pending.done <- *new(T)
		}
	}
}

// ScanAll reads every page of a paginated scan in page order.
func ScanAll[T any](scan func(cursor Cursor) (Page[T], error)) ([]T, error) {
	var items []T
	var cursor Cursor
	for {
		page, err := scan(cursor)
		if err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		cursor = page.Next
		if cursor == nil {
			return items, nil
		}
	}
}

// ClosedError is the harness-closed failure.
func ClosedError() error { return errors.New("Harness is closed") }
