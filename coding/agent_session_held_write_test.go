package coding

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// A483: an idle bash append fixes the entry's place under the run-lifecycle
// lock (D438) but makes the synchronous session-file write, on a session with
// no write queue, after releasing it, so a slow disk cannot stall a run start
// or an abort. Upstream recordBashResult (core/agent-session.ts, pi v1.5.0
// f9a26365) appends on its one event loop, where there is no lock to hold.
func TestIdleBashAppendWritesTheSessionFileOutsideTheRunLock(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session := newPromptSession(t, model, unusedSessionStreamFn)
	manager := newTestSessionManager(t)
	manager.AppendMessage(createUserMessage("started")) // the file exists from here on
	session.Sessions = manager

	entered, release := make(chan struct{}), make(chan struct{})
	original := sessionLineWriter
	sessionLineWriter = func(path, line string) error {
		close(entered)
		<-release
		return original(path, line)
	}
	t.Cleanup(func() { sessionLineWriter = original })

	done := make(chan struct{})
	go func() {
		defer close(done)
		session.RecordBashResult("echo hi", BashResult{Output: "hi", ExitCode: new(0)}, false)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("the append never reached the file write")
	}
	state := session.prompt()
	free := state.mu.TryLock()
	if free {
		state.mu.Unlock()
	}
	close(release)
	<-done
	if !free {
		t.Fatal("the run-lifecycle lock is held across the session file write")
	}
	if text := readSessionFileText(t, manager); !strings.Contains(text, "echo hi") {
		t.Fatalf("the bash result was not written once the append returned:\n%s", text)
	}
}

// A held write keeps its place in the file: a later synchronous append writes
// the held line first, and the flush that follows writes nothing twice.
func TestHeldSessionWriteKeepsFileOrder(t *testing.T) {
	manager := newTestSessionManager(t)
	manager.AppendMessage(createUserMessage("started"))
	manager.AppendMessageHeld(createUserMessage("held-line"))
	if text := readSessionFileText(t, manager); strings.Contains(text, "held-line") {
		t.Fatal("a held append wrote the file before the flush")
	}
	manager.AppendModelChange("anthropic", "later-line")
	manager.FlushHeldWrites()
	text := readSessionFileText(t, manager)
	held, later := strings.Index(text, "held-line"), strings.Index(text, "later-line")
	if held < 0 || later < 0 || held > later || strings.Count(text, "held-line") != 1 {
		t.Fatalf("held line at %d, later line at %d:\n%s", held, later, text)
	}
}

// FlushWrites promises every accepted write is on disk, and a held one is
// accepted: a caller that flushes and then reads the file sees it.
func TestFlushWritesIncludesHeldSessionWrites(t *testing.T) {
	manager := newTestSessionManager(t)
	manager.AppendMessage(createUserMessage("started"))
	manager.AppendMessageHeld(createUserMessage("held-line"))
	if err := manager.FlushWrites(); err != nil {
		t.Fatal(err)
	}
	if text := readSessionFileText(t, manager); strings.Count(text, "held-line") != 1 {
		t.Fatalf("the held line is not on disk after FlushWrites:\n%s", text)
	}
}

func readSessionFileText(t *testing.T, manager *SessionManager) string {
	t.Helper()
	data, err := os.ReadFile(manager.GetSessionFile())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
