package coding

import (
	"context"
	"testing"

	"github.com/dat267/pier/ai"
)

func recordSelectionSession(t *testing.T) *AgentSession {
	t.Helper()
	dir := t.TempDir()
	settings := NewSettingsManagerFromFiles(dir, dir, SettingsManagerCreateOptions{})
	model := &ai.Model{
		ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic",
		Reasoning: true, ContextWindow: 100000, MaxTokens: 100,
		ThinkingLevelMap: ai.ThinkingLevelMap{ai.ThinkMax: nil},
	}
	return newControlSession(t, model, settings)
}

func thinkingLevelEntries(session *AgentSession) []ai.ThinkingLevel {
	var levels []ai.ThinkingLevel
	for _, entry := range session.Sessions.GetEntries() {
		if entry.Type == "thinking_level_change" {
			levels = append(levels, ai.ThinkingLevel(entry.ThinkingLevel))
		}
	}
	return levels
}

// pi v1.5.2 (b4afdddf) agent-session.ts _recordThinkingLevel (#11451): a prompt
// records the live thinking level on the current branch when the latest recorded
// one belongs to another branch, so a resume restores what the session runs. The
// comparison uses the level clamped to the current model.
func TestPromptRecordsTheLiveThinkingLevel(t *testing.T) {
	session := recordSelectionSession(t)
	defer session.Dispose()

	// A level left on the branch by tree navigation, while the session runs low.
	session.Sessions.AppendThinkingLevelChange(ai.ThinkHigh)
	session.Agent.SetThinkingLevel(ai.ThinkLow)

	if err := session.Prompt(context.Background(), "hello", nil); err != nil {
		t.Fatal(err)
	}
	levels := thinkingLevelEntries(session)
	if len(levels) == 0 || levels[len(levels)-1] != ai.ThinkLow {
		t.Fatalf("recorded levels = %v; want the live level last", levels)
	}
}

// A branch that already records the live level gets no new entry, so a prompt
// does not grow the transcript every turn.
func TestPromptDoesNotRerecordTheSameThinkingLevel(t *testing.T) {
	session := recordSelectionSession(t)
	defer session.Dispose()

	session.Sessions.AppendThinkingLevelChange(ai.ThinkLow)
	session.Agent.SetThinkingLevel(ai.ThinkLow)

	if err := session.Prompt(context.Background(), "hello", nil); err != nil {
		t.Fatal(err)
	}
	if levels := thinkingLevelEntries(session); len(levels) != 1 || levels[0] != ai.ThinkLow {
		t.Fatalf("recorded levels = %v; want the single existing entry", levels)
	}
}
