package coding

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// Upstream agent-session.ts (pi v1.5.0 f9a26365) abort() aborts retry,
// compaction, branch summary and the agent run, not a user bash command; only
// dispose (and the dedicated abortBash) stop that. So Escape during a turn and
// /compact leave a running `!command` alone, while AbortAsync, used for
// teardown, still stops it.
func TestAbortLeavesAUserBashCommandRunning(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no shell available")
	}
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic"}
	session, err := NewAgentSession(&SessionConfig{Cwd: t.TempDir(), Model: model, StreamFn: stubStreamFn})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = session.ExecuteBash(context.Background(), "sleep 30", nil, nil)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !session.IsBashRunning() {
		if time.Now().After(deadline) {
			t.Fatal("the bash command never started")
		}
		time.Sleep(time.Millisecond)
	}

	session.Abort(context.Background())
	session.AbortTurnAsync()
	// The kill is asynchronous: give a wrongly stopped command time to end.
	select {
	case <-done:
		t.Fatal("aborting the turn stopped the user's bash command")
	case <-time.After(300 * time.Millisecond):
	}

	session.AbortAsync()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("AbortAsync did not stop the bash command")
	}
}
