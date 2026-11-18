package coding

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// switch_session opens the target file before it settles the outgoing session
// (upstream agent-session-runtime.ts teardownCurrent finishes before
// createRuntime opens the file). A run in flight keeps writing until it
// settles, so it can create the target after the open; the opened manager then
// still believes the file was absent, and its first write takes the
// exclusive-claim path against a file that is already there. The shutdown flush
// reports that as "session append failed ... file exists" (D493, CI run
// 44230627195).
func TestRPCSwitchReopensATargetCreatedWhileTheOutgoingRunSettles(t *testing.T) {
	cwd := t.TempDir()
	persist := true
	targetPath := filepath.Join(t.TempDir(), "other.jsonl")
	session, err := NewAgentSession(&SessionConfig{
		Cwd: cwd, StreamFn: stubStreamFn,
		Sessions: NewSessionManager(cwd, &SessionManagerOptions{SessionDir: t.TempDir(), Persist: &persist}),
	})
	if err != nil {
		t.Fatal(err)
	}

	var built *AgentSession
	model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	replace := func(_ context.Context, manager *SessionManager) (*AgentSession, error) {
		created, err := NewAgentSession(&SessionConfig{Cwd: manager.GetCwd(), Model: model, StreamFn: stubStreamFn, Sessions: manager})
		if err != nil {
			return nil, err
		}
		created.Agent.SetMessages(manager.Projection().Messages)
		built = created
		return created, nil
	}
	client := startRPCTestWith(t, session, RPCModeOptions{ReplaceSession: replace})

	// The first switch binds a session for the target, which does not exist yet.
	client.send(t, `{"id":"s1","type":"switch_session","sessionPath":`+strconv.Quote(targetPath)+`}`)
	if response := client.until(t, byResponseID("s1")); response["success"] != true {
		t.Fatalf("first switch response = %+v", response)
	}
	if built == nil {
		t.Fatal("the replacement factory did not run")
	}

	// Hold the next run before its first request: the run's user message has
	// not been appended, so the target is still absent.
	entered := make(chan struct{})
	release := make(chan struct{})
	built.SetBeforeFirstTurn(func(ctx context.Context) {
		close(entered)
		<-release
	})
	client.send(t, `{"id":"p1","type":"prompt","message":"hello"}`)
	if response := client.until(t, byResponseID("p1")); response["success"] != true {
		t.Fatalf("prompt response = %+v", response)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("the run did not reach its first turn")
	}

	// The switch opens the still-absent target, then settles the held run,
	// whose append creates the target before the replacement is bound.
	client.send(t, `{"id":"s2","type":"switch_session","sessionPath":`+strconv.Quote(targetPath)+`}`)
	time.Sleep(100 * time.Millisecond) // the open precedes the settle; let it get there
	close(release)
	if response := client.until(t, byResponseID("s2")); response["success"] != true {
		t.Fatalf("second switch response = %+v", response)
	}

	// The rebound session must write to the target without losing the claim.
	client.send(t, `{"id":"p2","type":"prompt","message":"after"}`)
	if response := client.until(t, byResponseID("p2")); response["success"] != true {
		t.Fatalf("prompt after the switch response = %+v", response)
	}
	client.until(t, func(record map[string]any) bool { return record["type"] == "agent_settled" })
	if err := built.Sessions.FlushWrites(); err != nil {
		t.Fatalf("write after the switch: %v", err)
	}
	// The replacement loaded the settled file, so both turns are in it.
	data := readFileBytes(t, targetPath)
	for _, want := range []string{"hello", "after"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("session file is missing %q:\n%s", want, data)
		}
	}
}
