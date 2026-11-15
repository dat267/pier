package interactive

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/internal/offloop"
)

// errTestExport is a failing export.
var errTestExport = errors.New("disk full")

// Exporting a long session re-chains and re-marshals every entry, which froze
// the UI for hundreds of milliseconds. The work must reach the owner loop with
// the export done off it (D-row in DECISIONS.md).
func TestExportCommandRunsOffTheOwnerLoop(t *testing.T) {
	InitTheme("dark", false)
	wiring, session, _ := newCommandTestWiring(t)
	var statuses []string
	wiring.ShowStatus = func(message string) { statuses = append(statuses, message) }
	wiring.ShowError = func(message string) { t.Errorf("unexpected error: %s", message) }

	queue := offloop.New()
	t.Cleanup(queue.Stop)
	posted := make(chan func(), 1)
	wiring.ExportQueue = queue
	wiring.PostToUI = func(fn func()) { posted <- fn }

	wiring.HandleExportCommand(context.Background(), "/export out.jsonl")
	// The completion status only arrives through PostToUI. session.exported is
	// written by the worker, so it is read after the callback runs, not here.
	if len(statuses) != 1 || statuses[0] != "Exporting session…" {
		t.Fatalf("statuses right after the command = %v, want the in-progress line only", statuses)
	}

	var callback func()
	select {
	case callback = <-posted:
	case <-time.After(5 * time.Second):
		t.Fatal("the export never reached the owner loop")
	}
	callback()
	if len(session.exported) != 1 || session.exported[0] != "out.jsonl" {
		t.Fatalf("exported = %v", session.exported)
	}
	if len(statuses) == 0 || statuses[len(statuses)-1] != "Session exported to: out.jsonl" {
		t.Fatalf("statuses = %v", statuses)
	}
}

// The synchronous path stays available for tests and embedders, and an export
// failure still reports as an error.
func TestExportCommandWithoutAQueueStaysSynchronous(t *testing.T) {
	InitTheme("dark", false)
	wiring, session, _ := newCommandTestWiring(t)
	var statuses, errors []string
	wiring.ShowStatus = func(message string) { statuses = append(statuses, message) }
	wiring.ShowError = func(message string) { errors = append(errors, message) }

	wiring.HandleExportCommand(context.Background(), "/export out.jsonl")
	if len(session.exported) != 1 {
		t.Fatalf("exported = %v", session.exported)
	}
	if len(statuses) != 1 || statuses[0] != "Session exported to: out.jsonl" {
		t.Fatalf("statuses = %v", statuses)
	}

	session.exportErr = errTestExport
	errors = nil
	wiring.HandleExportCommand(context.Background(), "/export out.jsonl")
	if len(errors) != 1 || errors[0] != "Failed to export session: "+errTestExport.Error() {
		t.Fatalf("errors = %v", errors)
	}
}

// The export must describe the session the command was issued on: it runs on the
// export queue, and the owner loop can replace the session before the worker runs
// (/new, /resume), the same reason forkWork snapshots its manager. Reading
// w.Session from the worker exported the replacement under the old command, and
// the promoted method read the loop-written embedded pointer off the loop.
//
// The ownership rule is AGENTS.md "UI ownership": a worker never touches
// attached UI state, and the owner loop is the only writer of the session wiring.
func TestExportCommandUsesTheDispatchedSession(t *testing.T) {
	app, cleanup := newTestAppPersisted(t)
	defer cleanup()
	appendTurn(t, app, "hello")

	queue := offloop.New()
	t.Cleanup(queue.Stop)
	gate := make(chan struct{})
	queue.Go(func() { <-gate })
	posted := make(chan func(), 1)
	app.commands.ExportQueue = queue
	app.commands.PostToUI = func(fn func()) { posted <- fn }

	outputPath := filepath.Join(t.TempDir(), "export.jsonl")
	app.commands.HandleExportCommand(context.Background(), "/export "+outputPath)
	// The owner loop replaces the session before the queued export runs.
	if _, err := app.sessionNew(); err != nil {
		t.Fatalf("new session: %v", err)
	}
	close(gate)
	queue.Flush()
	select {
	case fn := <-posted:
		fn()
	case <-time.After(10 * time.Second):
		t.Fatal("the export never reached the owner loop")
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if !strings.Contains(string(content), "hello") {
		t.Fatalf("the export does not hold the dispatched session's message: %s", content)
	}
}
