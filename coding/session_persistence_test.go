package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/internal/offloop"
)

// Recovery uses the prefix captured for each FIFO task, not the manager's
// eventual contents, so already queued saves cannot duplicate later entries.
func TestSessionQueuedRecoveryUsesAcceptedPrefixes(t *testing.T) {
	dir := t.TempDir()
	queue := offloop.New()
	defer queue.Stop()
	manager := NewSessionManager(dir, &SessionManagerOptions{SessionDir: dir, WriteQueue: queue})
	path := manager.GetSessionFile()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	ids := []string{manager.AppendMessage(createUserMessage("first"))}
	repaired := make(chan error, 1)
	queue.Go(func() { repaired <- os.Remove(path) })
	ids = append(ids, manager.AppendMessage(createUserMessage("second")))
	ids = append(ids, manager.AppendMessage(createUserMessage("third")))
	if err := manager.FlushWrites(); err != nil {
		t.Fatal(err)
	}
	if err := <-repaired; err != nil {
		t.Fatal(err)
	}
	failures := manager.DrainWriteErrors()
	if len(failures) != 1 {
		t.Fatalf("failure diagnostics = %+v, want original failed rewrite", failures)
	}
	reopened, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	entries := reopened.GetEntries()
	if len(entries) != len(ids) {
		t.Fatalf("persisted entries = %d, want %d", len(entries), len(ids))
	}
	for i, entry := range entries {
		if entry.ID != ids[i] {
			t.Fatalf("entry %d = %q, want %q", i, entry.ID, ids[i])
		}
	}
}

// Error notifications coalesce without blocking accepted writes. A subscriber
// also wakes for failures that were recorded before it obtained the channel.
func TestSessionWriteFailureNotifiesConsumer(t *testing.T) {
	dir := t.TempDir()
	manager := NewSessionManager(dir, &SessionManagerOptions{SessionDir: dir})
	if err := os.Mkdir(manager.GetSessionFile(), 0o755); err != nil {
		t.Fatal(err)
	}
	manager.AppendMessage(createUserMessage("first failed save"))
	source, ok := any(manager).(interface{ WriteErrorsReady() <-chan struct{} })
	if !ok {
		t.Fatal("session persistence has no nonblocking error notification")
	}
	ready := source.WriteErrorsReady()
	select {
	case <-ready:
	default:
		t.Fatal("preexisting failure did not notify consumer")
	}
	manager.DrainWriteErrors()
	manager.AppendMessage(createUserMessage("second failed save"))
	select {
	case <-ready:
	default:
		t.Fatal("later failure did not notify consumer")
	}
}

// Invalid entries must fail persistence, not disappear from a successful file.
// Upstream JSON.stringify does not silently omit a failed session entry.
func TestSessionSerializationFailureDoesNotSkipEntries(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "queued"}[queued], func(t *testing.T) {
			dir := t.TempDir()
			options := &SessionManagerOptions{SessionDir: dir}
			if queued {
				options.WriteQueue = offloop.New()
				t.Cleanup(options.WriteQueue.Stop)
			}
			manager := NewSessionManager(dir, options)
			manager.AppendMessage(createUserMessage("seed"))
			if err := manager.FlushWrites(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(manager.GetSessionFile())
			if err != nil {
				t.Fatal(err)
			}
			manager.AppendCustomEntry("invalid", json.RawMessage(`{`))
			if err := manager.FlushWrites(); err == nil {
				t.Fatal("serialization failure was swallowed")
			}
			manager.AppendMessage(createUserMessage("after invalid entry"))
			if err := manager.FlushWrites(); err == nil {
				t.Fatal("later save silently skipped the invalid entry")
			}
			after, err := os.ReadFile(manager.GetSessionFile())
			if err != nil || string(after) != string(before) {
				t.Fatalf("failed serialization changed existing file: %v", err)
			}
		})
	}
}

// Directory creation failures are visible even before the first conversation.
func TestSessionDirectoryFailureIsReported(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := NewSessionManager(dir, &SessionManagerOptions{SessionDir: filepath.Join(blocked, "sessions")})
	if err := manager.FlushWrites(); err == nil {
		t.Fatal("directory creation failure was swallowed")
	}
}

// Upstream core/session-manager.ts's filesystem calls throw on failure.
// D210: queued Go writes report failures through FlushWrites and DrainWriteErrors
// instead of throwing on the UI owner goroutine.
func TestSessionInitialWriteFailureIsReported(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "queued"}[queued], func(t *testing.T) {
			dir := t.TempDir()
			options := &SessionManagerOptions{SessionDir: dir}
			if queued {
				options.WriteQueue = offloop.New()
				t.Cleanup(options.WriteQueue.Stop)
			}
			manager := NewSessionManager(dir, options)
			// A directory at the destination fails on every OS, even as root.
			if err := os.Mkdir(manager.GetSessionFile(), 0o755); err != nil {
				t.Fatal(err)
			}
			manager.AppendMessage(createUserMessage("save me"))
			if err := manager.FlushWrites(); err == nil || !strings.Contains(err.Error(), filepath.Base(manager.GetSessionFile())) {
				t.Fatalf("flush error = %v, want destination write failure", err)
			}
		})
	}
}

// A failed initial write or append must not turn subsequent writes into a
// headerless or incomplete session. The next save retries the complete prefix.
func TestSessionWriteRecoveryPreservesAcceptedEntries(t *testing.T) {
	for _, queued := range []bool{false, true} {
		for _, initial := range []bool{false, true} {
			name := map[bool]string{false: "sync", true: "queued"}[queued] + "/" + map[bool]string{false: "append", true: "initial"}[initial]
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				options := &SessionManagerOptions{SessionDir: dir}
				if queued {
					options.WriteQueue = offloop.New()
					t.Cleanup(options.WriteQueue.Stop)
				}
				manager := NewSessionManager(dir, options)
				path := manager.GetSessionFile()
				var ids []string
				if !initial {
					ids = append(ids, manager.AppendMessage(createUserMessage("before failure")))
					if err := manager.FlushWrites(); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
				ids = append(ids, manager.AppendMessage(createUserMessage("during failure")))
				if err := manager.FlushWrites(); err == nil {
					t.Fatal("failed write was not reported")
				}
				failures := manager.DrainWriteErrors()
				if len(failures) != 1 || failures[0].Path != path || failures[0].Error == nil {
					t.Fatalf("write errors = %+v", failures)
				}
				if err := manager.FlushWrites(); err == nil {
					t.Fatal("draining diagnostics cleared unresolved write failure")
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				ids = append(ids, manager.AppendMessage(createUserMessage("after failure")))
				if err := manager.FlushWrites(); err != nil {
					t.Fatal(err)
				}
				reopened, err := OpenSession(path, dir, "")
				if err != nil {
					t.Fatal(err)
				}
				if reopened.GetHeader().ID != manager.GetSessionID() {
					t.Fatal("session header lost during write recovery")
				}
				entries := reopened.GetEntries()
				if len(entries) != len(ids) {
					t.Fatalf("persisted entries = %d, want %d", len(entries), len(ids))
				}
				for i, entry := range entries {
					if entry.ID != ids[i] {
						t.Fatalf("entry %d = %q, want %q", i, entry.ID, ids[i])
					}
				}
			})
		}
	}
}
