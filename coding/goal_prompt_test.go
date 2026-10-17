package coding

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dat267/pier/agent"
)

// The goal tools contribute their snippet and guideline bullets to the system
// prompt. Upstream `_rebuildSystemPrompt` reads `promptSnippet` and
// `promptGuidelines` from every tool definition, including extension tools, so
// the goal guidance reaches the `<tools>` and `<rules>` sections.
func TestGoalToolsContributeToSystemPrompt(t *testing.T) {
	var prompts atomic.Int64
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Goal: true, Tools: []agent.AgentTool{testTool("read")},
		StreamFn: recordingStreamFn(t, &prompts),
	})
	if err != nil {
		t.Fatal(err)
	}

	snippets, guidelines := session.toolPromptContributions()
	if snippets["set_goal"] != "Set, replace, or end the session goal" {
		t.Fatalf("set_goal snippet = %q", snippets["set_goal"])
	}
	for _, name := range []string{"set_goal", "get_goal"} {
		if len(guidelines[name]) == 0 {
			t.Fatalf("goal tool %s has no guidelines: %+v", name, guidelines)
		}
	}

	prompt := session.SystemPrompt()
	if !strings.Contains(prompt, "set_goal: Set, replace, or end the session goal") {
		t.Fatalf("system prompt missing the set_goal snippet:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Use set_goal with an objective when the user") {
		t.Fatalf("system prompt missing a set_goal guideline:\n%s", prompt)
	}
}

// While a goal is being pursued, get_goal joins the active set and its guidance
// reaches the prompt too.
func TestPursuedGoalToolsContributeToSystemPrompt(t *testing.T) {
	var prompts atomic.Int64
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Goal: true, Tools: []agent.AgentTool{testTool("read")},
		StreamFn: recordingStreamFn(t, &prompts),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.control.Tools["set_goal"].Tool.Execute("create",
		[]byte(`{"objective":"finish it"}`), context.Background(), func(agent.AgentToolResult) {}); err != nil {
		t.Fatal(err)
	}
	prompt := session.SystemPrompt()
	if !strings.Contains(prompt, "get_goal: Read the current goal") {
		t.Fatalf("system prompt missing the get_goal snippet:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Read get_goal before set_goal") {
		t.Fatalf("system prompt missing a get_goal guideline:\n%s", prompt)
	}
}
