package coding

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/dat267/pier/agent"
)

// pi v1.4.0 (4a080252), core/agent-session.ts _rebuildSystemPrompt and
// _refreshToolRegistry execute on one JS thread. Late MCP attachment runs on
// a Go worker, so prompt resolution must read the registry under its lock.
func TestSystemPromptCanReadDuringMcpToolAttachment(t *testing.T) {
	dir := t.TempDir()
	tools := []agent.AgentTool{{Name: "read", Parameters: []byte(`{"type":"object"}`)}}
	for i := range 64 {
		tools = append(tools, agent.AgentTool{Name: fmt.Sprintf("fixture_%d", i), Parameters: []byte(`{"type":"object"}`)})
	}
	session, err := NewAgentSession(&SessionConfig{Cwd: dir, Sessions: InMemorySession(dir, nil, nil), StreamFn: stubStreamFn, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Go(func() {
		<-start
		for range 128 {
			session.AttachExtraTools([]agent.AgentTool{{Name: "mcp__fixture__late", Parameters: []byte(`{"type":"object"}`)}})
			session.AttachExtraTools(nil)
		}
	})
	wait.Go(func() {
		<-start
		for range 128 {
			if prompt := session.SystemPrompt(); !strings.Contains(prompt, "Read file contents") {
				t.Error("stable read tool disappeared from the prompt")
				return
			}
		}
	})
	close(start)
	wait.Wait()
}
