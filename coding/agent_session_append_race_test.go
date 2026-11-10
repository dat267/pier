package coding

import (
	"strconv"
	"sync"
	"testing"

	"github.com/dat267/pier/ai"
)

// Bash results recorded at once on an idle session (SDK and RPC callers can
// run several commands) all reach the transcript: each append is one step on
// the agent's messages, not a read followed by a later replace that drops
// whatever another append added in between. Upstream recordBashResult
// (agent-session.ts, v1.5.0 f9a26365) appends synchronously on one event loop.
func TestConcurrentBashResultsAreAllRecorded(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session := newPromptSession(t, model, unusedSessionStreamFn)
	manager, _ := newTestSession(t)
	session.Sessions = manager
	const commands = 32
	var group sync.WaitGroup
	start := make(chan struct{})
	for index := range commands {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			session.RecordBashResult("echo "+strconv.Itoa(index), BashResult{Output: "ok", ExitCode: new(0)}, false)
		}()
	}
	close(start)
	group.Wait()
	got := 0
	for _, message := range session.Agent.State().Messages {
		if ai.RoleOf(message) == RoleBashExecution {
			got++
		}
	}
	if got != commands {
		t.Fatalf("transcript has %d of %d bash results", got, commands)
	}
}
