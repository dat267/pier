package coding

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// pi v1.4.0 (4a080252), core/auth-storage.ts withLockAsync checks
// onCompromised before committing; proper-lockfile owns its lock lease. A
// suspended process may resume after another process reclaimed its stale lock.
// The old operation must neither commit nor remove the successor's live lock.
func TestAuthStorageLostLeaseDoesNotCommitOrReleaseSuccessor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	const original = `{"original":{"type":"api_key","key":"synthetic-original"}}`
	writeAuthFile(t, path, original)
	backend := NewFileAuthStorageBackend(path)
	entered, finishFirst := make(chan struct{}), make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := backend.WithLockAsync(context.Background(), func(*string) (LockResult, error) {
			close(entered)
			<-finishFirst
			stale := `{"stale":{"type":"api_key","key":"synthetic-stale"}}`
			return LockResult{Next: &stale}, nil
		})
		firstDone <- err
	}()
	<-entered
	// Model stale recovery without waiting 30 seconds. Retain the old inode
	// so an immediately recreated directory cannot reuse its identity.
	lockPath := path + ".lock"
	if err := os.Rename(lockPath, lockPath+".expired"); err != nil {
		close(finishFirst)
		<-firstDone
		t.Fatal(err)
	}
	secondEntered, finishSecond := make(chan struct{}), make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		_, err := NewFileAuthStorageBackend(path).WithLockAsync(context.Background(), func(*string) (LockResult, error) {
			close(secondEntered)
			<-finishSecond
			current := `{"current":{"type":"api_key","key":"synthetic-current"}}`
			return LockResult{Next: &current}, nil
		})
		secondDone <- err
	}()
	<-secondEntered
	close(finishFirst)
	firstErr := <-firstDone
	raw, readErr := os.ReadFile(path)
	_, lockErr := os.Stat(lockPath)
	close(finishSecond)
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	if firstErr == nil {
		t.Error("operation committed after losing its lock lease")
	}
	if readErr != nil || string(raw) != original {
		t.Errorf("old holder changed auth.json while successor owned the lock: %q, %v", raw, readErr)
	}
	if lockErr != nil {
		t.Errorf("old holder removed successor's lock: %v", lockErr)
	}
}
