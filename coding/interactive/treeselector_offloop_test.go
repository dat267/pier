package interactive

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/internal/offloop"
)

// openTreeTestSession writes a session file with messages and opens it.
func openTreeTestSession(t *testing.T, messages int) *coding.SessionManager {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	lines := `{"type":"session","version":3,"id":"s1","timestamp":"2026-01-01T00:00:00.000Z","cwd":"/tmp"}` + "\n"
	parent := ""
	for index := range messages {
		id := "m" + string(rune('a'+index))
		parentField := ""
		if parent != "" {
			parentField = `,"parentId":"` + parent + `"`
		}
		lines += `{"type":"message","id":"` + id + `","timestamp":"2026-01-01T00:00:0` + string(rune('0'+index)) + `.000Z"` + parentField +
			`,"message":{"role":"user","content":[{"type":"text","text":"hello ` + id + `"}]}}` + "\n"
		parent = id
	}
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := coding.OpenSession(path, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// A long session's tree decodes every entry, which is hundreds of milliseconds
// on a real session. The open must reach the owner loop with the work done off
// it: /tree freezes the UI otherwise (D-row pending, see DECISIONS.md).
func TestTreeSelectorOpenBuildsOffTheOwnerLoop(t *testing.T) {
	InitTheme("dark", false)
	wiring, _, _, _ := newSelectorTestWiring(t)
	wiring.SessionInfo = openTreeTestSession(t, 3)

	queue := offloop.New()
	t.Cleanup(queue.Stop)
	var posted []func()
	waiting := make(chan struct{}, 1)
	wiring.TreeQueue = queue
	wiring.PostToUI = func(fn func()) {
		posted = append(posted, fn)
		select {
		case waiting <- struct{}{}:
		default:
		}
	}

	wiring.ShowTreeSelector(context.Background(), "", false)
	if wiring.Slot.HasActiveSelector() {
		t.Fatal("the selector opened synchronously; the tree build must run off the owner loop")
	}

	select {
	case <-waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("the tree build never reached the owner loop")
	}
	if len(posted) != 1 {
		t.Fatalf("posted callbacks = %d, want 1", len(posted))
	}
	posted[0]()
	if !wiring.Slot.HasActiveSelector() {
		t.Fatal("the selector did not open after the build completed")
	}
}

// The synchronous path stays available for tests and embedders, and an empty
// session still reports instead of opening an empty selector.
func TestTreeSelectorOpenWithoutAQueueStaysSynchronous(t *testing.T) {
	InitTheme("dark", false)
	wiring, _, _, _ := newSelectorTestWiring(t)
	wiring.SessionInfo = openTreeTestSession(t, 2)
	wiring.ShowTreeSelector(context.Background(), "", false)
	if !wiring.Slot.HasActiveSelector() {
		t.Fatal("the selector did not open synchronously")
	}

	wiring, _, _, _ = newSelectorTestWiring(t)
	wiring.SessionInfo = openTreeTestSession(t, 0)
	statuses := ""
	wiring.ShowStatus = func(message string) { statuses += message }
	wiring.ShowTreeSelector(context.Background(), "", false)
	if wiring.Slot.HasActiveSelector() {
		t.Fatal("an empty session must not open a selector")
	}
	if statuses != "No entries in session" {
		t.Fatalf("status = %q", statuses)
	}
}
