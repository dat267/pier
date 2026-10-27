package coding

import (
	"context"
	"errors"
	"testing"
)

// pi v1.4.0 (4a080252), auth-storage.ts InMemoryAuthStorageBackend.withLockAsync
// checks signal.throwIfAborted after the callback and before committing next.
func TestMemoryAuthBackendDoesNotCommitCancelledCallback(t *testing.T) {
	backend := NewInMemoryAuthStorageBackend()
	original := "old"
	if _, err := backend.WithLock(func(*string) (LockResult, error) {
		return LockResult{Next: &original}, nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := backend.WithLockAsync(ctx, func(*string) (LockResult, error) {
		cancel()
		next := "new"
		return LockResult{Next: &next}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("callback cancellation = %v, want context.Canceled", err)
	}
	// The synchronous read also waits until the worker releases its data lock,
	// so the check observes the settled operation even if cancellation won early.
	value, err := backend.WithLock(func(current *string) (LockResult, error) {
		if current == nil {
			return LockResult{}, nil
		}
		return LockResult{Result: *current}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if value != "old" {
		t.Fatalf("cancelled callback committed %v, want old state", value)
	}
}
