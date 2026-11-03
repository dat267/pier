package coding

import (
	"runtime"
	"sync"
	"testing"
	"time"
	"weak"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.5.0 (f9a26365), core/session-manager.ts newSession
// drops the old entries. The Go memo must not let an already-admitted
// projection repopulate manager-owned storage after that retirement.
func TestConcurrentProjectionCannotRepopulateDiscardedSessionMemo(t *testing.T) {
	manager := InMemorySession(t.TempDir(), nil, nil)
	manager.AppendMessage(createUserMessage("old first"))
	manager.AppendMessage(createUserMessage("old second"))
	entered := make(chan struct{})
	release := make(chan struct{})
	var decodeOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	manager.SetMessageDecodeForTest(func(entry *SessionEntry) []ai.Message {
		decodeOnce.Do(func() {
			close(entered)
			<-release
		})
		return SessionEntryToContextMessages(entry)
	})
	result := make(chan []weak.Pointer[ai.UserMessage], 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		projection := manager.Projection()
		var pointers []weak.Pointer[ai.UserMessage]
		for _, message := range projection.Messages {
			if user, ok := message.(*ai.UserMessage); ok {
				pointers = append(pointers, weak.Make(user))
			}
		}
		result <- pointers
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("projection did not reach the decoder barrier")
	}
	manager.NewSession(nil)
	unblock()
	var pointers []weak.Pointer[ai.UserMessage]
	select {
	case pointers = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("old projection did not finish")
	}
	<-done
	if len(pointers) != 2 {
		t.Fatalf("old snapshot returned %d user messages, want 2", len(pointers))
	}
	runtime.GC()
	for i, pointer := range pointers {
		if pointer.Value() != nil {
			t.Errorf("discarded decoded message %d is retained by a late projection", i)
		}
	}
	runtime.KeepAlive(manager)
}
