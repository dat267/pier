package durable

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Port of harness/output.ts.

func TestSanitizeOutput(t *testing.T) {
	if got := SanitizeOutput("a\tb\nc"); got != "a\tb\nc" {
		t.Fatalf("kept = %q", got)
	}
	if got := SanitizeOutput("a\x00b\x07c\x0bd\x0ce\x0df\x1fg\uFFF9h"); got != "abcdefgh" {
		t.Fatalf("removed = %q", got)
	}
}

func TestBoundOutputHead(t *testing.T) {
	result := BoundOutput("a\nb\nc\nd\ne\n", OutputLimits{Retain: RetainHead, MaxLines: 2, MaxBytes: 1000})
	if result.Text != "a\nb\n" || result.DroppedLines != 3 || result.Bytes != 4 {
		t.Fatalf("result = %+v", result)
	}
}

func TestBoundOutputTail(t *testing.T) {
	result := BoundOutput("a\nb\nc\nd\ne\n", OutputLimits{Retain: RetainTail, MaxLines: 2, MaxBytes: 1000})
	if result.Text != "d\ne\n" || result.DroppedLines != 3 {
		t.Fatalf("result = %+v", result)
	}
}

func TestBoundOutputByteLimitAndCharacters(t *testing.T) {
	// A long line is cut at the byte limit without a partial character.
	result := BoundOutput("ééé", OutputLimits{Retain: RetainHead, MaxLines: 10, MaxBytes: 3})
	if result.Text != "é" || result.Bytes != 2 {
		t.Fatalf("head = %+v", result)
	}
	result = BoundOutput("ééé", OutputLimits{Retain: RetainTail, MaxLines: 10, MaxBytes: 3})
	if result.Text != "é" || result.Bytes != 2 {
		t.Fatalf("tail = %+v", result)
	}
	// Zero limits keep nothing.
	result = BoundOutput("abc", OutputLimits{Retain: RetainHead, MaxLines: 0, MaxBytes: 0})
	if result.Text != "" || result.DroppedBytes != 3 {
		t.Fatalf("zero = %+v", result)
	}
}

func TestOutputBufferHead(t *testing.T) {
	buffer := NewOutputBuffer(OutputLimits{Retain: RetainHead, MaxLines: 2, MaxBytes: 1000})
	buffer.Push("a\n")
	buffer.Push("b\n")
	buffer.Push("c\n")
	snapshot := buffer.Snapshot()
	if snapshot.Text != "a\nb\n" || snapshot.DroppedLines != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	// Once the head window is full, later chunks are not stored.
	if buffer.StoredBytes() > 1000 {
		t.Fatalf("stored = %d", buffer.StoredBytes())
	}
}

func TestOutputBufferTailDropsOldChunks(t *testing.T) {
	buffer := NewOutputBuffer(OutputLimits{Retain: RetainTail, MaxLines: 2, MaxBytes: 1000})
	for _, chunk := range []string{"a\n", "b\n", "c\n", "d\n", "e\n"} {
		buffer.Push(chunk)
	}
	snapshot := buffer.Snapshot()
	if snapshot.Text != "d\ne\n" || snapshot.DroppedLines != 3 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	// After a snapshot the stored window is the retained slice.
	if buffer.StoredBytes() != 4 {
		t.Fatalf("stored = %d", buffer.StoredBytes())
	}
	snapshot = buffer.Snapshot()
	if snapshot.Text != "d\ne\n" || snapshot.DroppedLines != 3 {
		t.Fatalf("second = %+v", snapshot)
	}
}

func TestOutputBufferTotalsAcrossPushes(t *testing.T) {
	buffer := NewOutputBuffer(OutputLimits{Retain: RetainTail, MaxLines: 1, MaxBytes: 1000})
	buffer.Push("one\n")
	buffer.Push("two\n")
	buffer.Push("three\n")
	snapshot := buffer.Snapshot()
	if snapshot.Text != "three\n" {
		t.Fatalf("text = %q", snapshot.Text)
	}
	if snapshot.DroppedLines != 2 || snapshot.DroppedBytes != 8 {
		t.Fatalf("dropped = %+v", snapshot)
	}
	// Sanitization applies at snapshot time.
	buffer = NewOutputBuffer(OutputLimits{Retain: RetainHead, MaxLines: 10, MaxBytes: 1000})
	buffer.Push("a\x00b")
	if snapshot := buffer.Snapshot(); snapshot.Text != "ab" {
		t.Fatalf("sanitized = %q", snapshot.Text)
	}
}

func TestProgressCommitsAndStops(t *testing.T) {
	var writes int32
	progress := NewProgress(func() (int, error) {
		atomic.AddInt32(&writes, 1)
		return 4, nil
	}, func(error) { t.Error("unexpected error") }, defaultProgressMinIntervalMs)
	progress.minIntervalMs, progress.bytesPerSecond = 50, 1_000_000_000
	// The first change after idle commits at once.
	if err := progress.MarkAndWait().Wait(); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&writes) != 1 {
		t.Fatalf("writes = %d", writes)
	}
	// The next change is delayed by the minimum interval.
	waiter := progress.MarkAndWait()
	select {
	case err := <-waiter.done:
		t.Fatalf("committed too early: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	if err := waiter.Wait(); err != nil {
		t.Fatal(err)
	}
	// Stop returns the still-pending waiters for the final commit to settle.
	pending := progress.MarkAndWait()
	waiters := progress.Stop()
	if len(waiters) != 1 {
		t.Fatalf("waiters = %d", len(waiters))
	}
	for _, waiter := range waiters {
		waiter.Resolve()
	}
	if err := pending.Wait(); err != nil {
		t.Fatal(err)
	}
}

// Port of configurable minimum progress pacing from
// packages/durable/src/harness/output.ts at pi v1.1.0 commit 674d64f09.
func TestProgressUsesConfiguredMinimumInterval(t *testing.T) {
	var writes int32
	progress := NewProgress(func() (int, error) {
		atomic.AddInt32(&writes, 1)
		return 0, nil
	}, func(error) { t.Error("unexpected error") }, 0)
	if err := progress.MarkAndWait().Wait(); err != nil {
		t.Fatal(err)
	}
	waiter := progress.MarkAndWait()
	select {
	case err := <-waiter.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("zero minimum interval still delayed progress")
	}
	if got := atomic.LoadInt32(&writes); got != 2 {
		t.Fatalf("writes = %d, want 2", got)
	}
	progress.Stop()
}

func TestProgressReportsError(t *testing.T) {
	wantErr := errors.New("write failed")
	reported := make(chan error, 1)
	progress := NewProgress(func() (int, error) { return 0, wantErr }, func(err error) { reported <- err })
	progress.minIntervalMs = 1
	if err := progress.MarkAndWait().Wait(); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v", err)
	}
	select {
	case err := <-reported:
		if !errors.Is(err, wantErr) {
			t.Fatalf("reported = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("onError not called")
	}
}
