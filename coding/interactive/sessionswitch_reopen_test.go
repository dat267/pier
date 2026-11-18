package interactive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
)

// D493: the target is opened off the owner loop before teardown settles the
// outgoing run, whose last append can create it. The rebind must reopen the
// target, or its first write claims a file that is already there.
func TestSwitchReopensATargetCreatedWhileTheOutgoingRunSettles(t *testing.T) {
	app, cleanup := newTestAppPersisted(t)
	defer cleanup()
	app.Init(context.Background())
	cwd := app.sessionMgr.GetCwd()
	model := app.session.AgentSession.Model()
	targetPath := filepath.Join(t.TempDir(), "other.jsonl")

	// Bind a session for the still-absent target.
	first, err := coding.OpenSession(targetPath, "", cwd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.applySessionReplacement(first); err != nil {
		t.Fatal(err)
	}
	// bindSessionManager resolves the model from the session and settings; a
	// fresh target has neither, and the run below needs one.
	app.session.AgentSession.Agent.SetModel(model)

	// Hold the next run before its first request: the run's user message has
	// not been appended, so the target is still absent.
	entered := make(chan struct{})
	outgoing := app.session.AgentSession
	outgoing.SetBeforeFirstTurn(func(ctx context.Context) {
		close(entered)
		<-ctx.Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	promptDone := make(chan error, 1)
	go func() { promptDone <- outgoing.Prompt(ctx, "hello", nil) }()
	select {
	case <-entered:
	case err := <-promptDone:
		t.Fatalf("prompt failed before its first turn: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("the run did not reach its first turn")
	}

	// The switch opens the still-absent target, then aborts the run; its append
	// creates the target before the rebind.
	result, err := app.switchSession(targetPath, cwd)
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	if result == nil || result.Cancelled {
		t.Fatalf("switch cancelled: %+v", result)
	}
	<-promptDone
	if err := app.sessionMgr.FlushWrites(); err != nil {
		t.Fatalf("write after the switch: %v", err)
	}
	if app.sessionMgr.SessionFileAppeared() {
		t.Fatal("the rebound manager still believes its file was absent")
	}
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("the settled turn is not in the target:\n%s", data)
	}
}
