package coding

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// Reference: pi f9a26365 core/session-manager.ts _setSessionFile checks
// existsSync, loads the entries, and for an empty parse calls statSync, whose
// ENOENT is thrown. A session file deleted while it is being opened must
// surface an error, never a nil dereference.
func TestOpenSessionSurvivesAFileDeletedWhileOpening(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	var stop atomic.Bool
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for !stop.Load() {
			_ = os.Remove(path)
			_ = os.WriteFile(path, nil, 0o600)
		}
	}()

	var panics atomic.Int64
	for range 20000 {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					panics.Add(1)
				}
			}()
			// Either outcome is valid while another goroutine removes the file:
			// a fresh manager for the empty file, or the vanished-file error.
			_, _ = OpenSession(path, "", "")
		}()
		if panics.Load() > 0 {
			break
		}
	}
	stop.Store(true)
	writer.Wait()
	if panics.Load() > 0 {
		t.Fatal("opening a session file deleted mid-open panicked")
	}

	// Controls: a stable empty file opens as a new session; stable invalid
	// content is rejected.
	stable := filepath.Join(dir, "stable.jsonl")
	if err := os.WriteFile(stable, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSession(stable, "", ""); err != nil {
		t.Fatalf("empty session file: %v", err)
	}
	invalid := filepath.Join(dir, "invalid.jsonl")
	if err := os.WriteFile(invalid, []byte("not a session\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSession(invalid, "", ""); err == nil {
		t.Fatal("invalid non-empty session file opened")
	}
}
