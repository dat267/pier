package agent

import (
	"sync"
	"testing"

	"github.com/dat267/pier/ai"
)

// Concurrent appenders each land in the transcript, and a State snapshot has
// no spare capacity, so appending to it cannot write into slots the agent's
// own transcript will reuse. Upstream mutates agent.state.messages on one
// event loop (packages/agent/src/agent.ts, v1.5.0 f9a26365).
func TestAppendMessagesKeepsEveryConcurrentAppend(t *testing.T) {
	a, err := NewAgent(&AgentOptions{StreamFn: unusedStreamFn()})
	if err != nil {
		t.Fatal(err)
	}
	const appenders = 32
	var group sync.WaitGroup
	start := make(chan struct{})
	for range appenders {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			a.AppendMessages(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "x"}})
		}()
	}
	close(start)
	group.Wait()
	messages := a.State().Messages
	if len(messages) != appenders {
		t.Fatalf("transcript has %d of %d appends", len(messages), appenders)
	}
	if cap(messages) != len(messages) {
		t.Fatalf("snapshot cap = %d, len = %d: appending to it would share the agent's array", cap(messages), len(messages))
	}
	_ = append(messages, &ai.UserMessage{Content: ai.StringOrBlocks{Text: "snapshot only"}})
	a.AppendMessages(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "agent"}})
	if last := a.State().Messages[appenders]; ai.RoleOf(last) != ai.RoleUser || last.(*ai.UserMessage).Content.Text != "agent" {
		t.Fatalf("the snapshot's append leaked into the transcript: %+v", last)
	}
}
