package coding

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dat267/pier/internal/offloop"
)

// The interactive wiring hands the manager a write queue: file writes run off
// the calling goroutine (the UI loop) in submission order, and FlushWrites
// makes them observable for tests and shutdown.

func TestSessionWritesLandInOrderOnTheQueue(t *testing.T) {
	dir := t.TempDir()
	m := NewSessionManager(dir, &SessionManagerOptions{
		SessionDir: filepath.Join(dir, "sessions"),
		WriteQueue: offloop.New(),
	})
	// The flush-on-first-conversation contract: the file is created by the
	// rewrite that fires when the first user or assistant message lands.
	m.AppendMessage(createAssistantMessageT("seed"))
	const n = 50
	want := []string{"session", "message"} // header, then the seed message
	for i := 0; i < n; i++ {
		m.AppendMessage(createUserMessage("message number"))
		m.AppendSessionInfo("name")
		want = append(want, "message", "session_info")
	}
	if err := m.FlushWrites(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(m.GetSessionFile())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	var got []string
	for scanner.Scan() {
		var entry struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("line %d: %v", len(got), err)
		}
		got = append(got, entry.Type)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	// The complete persisted order must match submission order exactly, not just
	// the final interleave: a reordered or duplicated middle entry fails here.
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted order = %v, want %v", got, want)
	}
}

func TestSessionFlushWritesIsNilSafe(t *testing.T) {
	dir := t.TempDir()
	m := NewSessionManager(dir, &SessionManagerOptions{SessionDir: filepath.Join(dir, "sessions")})
	m.AppendMessage(createUserMessage("sync"))
	m.FlushWrites() // must not panic without a queue
}
