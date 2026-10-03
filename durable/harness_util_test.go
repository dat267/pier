package durable

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Port of harness/util.ts.

func TestWaitersResolve(t *testing.T) {
	var waiters Waiters[string, int]
	done := make(chan int, 2)
	for index := 0; index < 2; index++ {
		go func() {
			value, err := waiters.Add("key", context.Background())
			if err != nil {
				t.Error(err)
			}
			done <- value
		}()
	}
	deadline := time.Now().Add(time.Second)
	for len(waiters.Keys()) < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	waiters.Resolve("key", 7)
	for index := 0; index < 2; index++ {
		if value := <-done; value != 7 {
			t.Fatalf("value = %d", value)
		}
	}
	if len(waiters.Keys()) != 0 {
		t.Fatalf("keys = %v", waiters.Keys())
	}
}

func TestWaitersRejectAllAndCancellation(t *testing.T) {
	var waiters Waiters[string, int]
	rejected := make(chan error, 1)
	go func() {
		_, err := waiters.Add("key", context.Background())
		rejected <- err
	}()
	deadline := time.Now().Add(time.Second)
	for len(waiters.Keys()) < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	wantErr := errors.New("boom")
	waiters.RejectAll(wantErr)
	if err := <-rejected; !errors.Is(err, wantErr) {
		t.Fatalf("err = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancelled := make(chan error, 1)
	go func() {
		_, err := waiters.Add("other", ctx)
		cancelled <- err
	}()
	deadline = time.Now().Add(time.Second)
	for len(waiters.Keys()) < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-cancelled; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestScanAll(t *testing.T) {
	pages := []Page[int]{
		{Items: []int{1, 2}, Next: Cursor{"after": []byte("2")}},
		{Items: []int{3}},
	}
	index := 0
	items, err := ScanAll(func(cursor Cursor) (Page[int], error) {
		page := pages[index]
		index++
		return page, nil
	})
	if err != nil || len(items) != 3 || items[0] != 1 || items[2] != 3 {
		t.Fatalf("items = %v, %v", items, err)
	}
}
