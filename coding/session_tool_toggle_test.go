package coding

import (
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// The /features tools section (D496) turns one registered tool on or off for the
// live session. Disabling removes it from the active set and the system prompt's
// tool section; enabling puts it back in its registry order. A tool that does not
// exist is reported, not silently added, and an explicit --tools allowlist still
// bounds what can be enabled.
func TestSessionToolToggle(t *testing.T) {
	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, ConvertToLlm: ConvertToLlm,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000},
		Tools: []agent.AgentTool{testTool("read"), testTool("bash"), testTool("edit")},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()
	active := func() string {
		names := ""
		for _, name := range session.GetActiveToolNames() {
			names += name + ","
		}
		return names
	}
	if got := active(); got != "read,bash,edit," {
		t.Fatalf("active = %q", got)
	}

	if err := session.SetToolEnabled("bash", false); err != nil {
		t.Fatal(err)
	}
	if got := active(); got != "read,edit," {
		t.Fatalf("active after disabling bash = %q", got)
	}
	// The prompt's tool section follows the active set.
	if names := session.promptToolNamesForTest(); names != "read,edit," {
		t.Fatalf("prompt tools after disabling bash = %q", names)
	}

	if err := session.SetToolEnabled("bash", true); err != nil {
		t.Fatal(err)
	}
	if got := active(); got != "read,edit,bash," {
		t.Fatalf("active after enabling bash = %q, want it appended in its registry order", got)
	}
	// Toggling a tool the session does not have is an error, not a new tool.
	if err := session.SetToolEnabled("nope", true); err == nil {
		t.Fatal("enabling an unknown tool succeeded")
	}
}

// promptToolNamesForTest reads the tool names the system prompt was built with.
func (s *AgentSession) promptToolNamesForTest() string {
	s.promptOptionsMu.Lock()
	defer s.promptOptionsMu.Unlock()
	if s.SystemPromptOptions == nil {
		return ""
	}
	names := ""
	for _, name := range s.SystemPromptOptions.SelectedTools {
		names += name + ","
	}
	return names
}
