package interactive

import (
	"context"
	"errors"
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
