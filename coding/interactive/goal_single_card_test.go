package interactive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// D223: user-requested native presentation. A successful set_goal objective
// already renders its outcome in the tool frame. The clear/create records must
// remain durable without adding duplicate cards (pi 4a080252 interactive-mode.ts
// renders tool frames and custom entries separately by default).
func TestGoalToolActionShowsOneLiveCard(t *testing.T) {
	for _, replace := range []bool{false, true} {
		name, outcome := "create", "Goal created ✓"
		if replace {
			name, outcome = "replace", "Goal replaced ✓"
		}
		t.Run(name, func(t *testing.T) {
			renderer, _ := newTranscriptTestRenderer(t, false)
			renderer.EntryRenderer = GoalEntryRenderer
			manager := coding.NewSessionManager(t.TempDir(), &coding.SessionManagerOptions{Persist: new(false)})
			session, err := coding.NewAgentSession(&coding.SessionConfig{
				Cwd: manager.GetCwd(), Sessions: manager, Goal: true,
				StreamFn: func(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
					t.Fatal("creating a goal must not start a model request")
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(session.Dispose)
			tool := session.GetToolDefinition("set_goal")
			if replace {
				if _, err := tool.Execute("seed", json.RawMessage(`{"objective":"old objective"}`), context.Background(), nil); err != nil {
					t.Fatal(err)
				}
			}
			dispatcher := NewEventDispatcher(renderer, nil, nil, renderer.Settings, nil, manager, nil)
			unsubscribe := session.Subscribe(dispatcher.HandleEvent)
			defer unsubscribe()
			args := json.RawMessage(`{"objective":"Ship the simplified goal card"}`)
			dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionToolExecutionStart,
				Agent: &agent.AgentEvent{ToolName: "set_goal", ToolCallID: "set", Args: args}})
			result, err := tool.Execute("set", args, context.Background(), nil)
			if err != nil || result.IsError {
				t.Fatalf("set_goal: result=%+v err=%v", result, err)
			}
			dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionToolExecutionEnd,
				Agent: &agent.AgentEvent{ToolName: "set_goal", ToolCallID: "set", Result: result}})
			got := coding.StripAnsi(strings.Join(renderer.Chat.Render(100), "\n"))
			if len(renderer.Chat.Children) != 1 || strings.Count(got, "Ship the simplified goal card") != 1 || !strings.Contains(got, outcome) {
				t.Fatalf("want one tool card with the objective and outcome, got %d cards:\n%s", len(renderer.Chat.Children), got)
			}
			wantRecords := 1
			if replace {
				wantRecords = 3 // seed create + replacement clear/create
			}
			records := 0
			for _, entry := range manager.GetBranch("") {
				if entry.Type == "custom" && entry.CustomType == "pi-goal" {
					records++
				}
			}
			if records != wantRecords {
				t.Fatalf("durable goal records = %d, want %d", records, wantRecords)
			}
		})
	}
}

// Reopening the persisted transcript must keep the same single-card view.
func TestGoalReplacementShowsOneReplayedCard(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	renderer.EntryRenderer = GoalEntryRenderer
	renderer.Session = &goalCardTranscriptSession{}
	manager := coding.NewSessionManager(t.TempDir(), &coding.SessionManagerOptions{Persist: new(false)})
	manager.AppendMessage(&ai.AssistantMessage{
		Content:    ai.ContentList{ai.ToolCall{ID: "set", Name: "set_goal", Arguments: json.RawMessage(`{"objective":"Ship the simplified goal card"}`)}},
		StopReason: ai.StopToolUse,
	})
	manager.AppendCustomEntry("pi-goal", json.RawMessage(`{"operation":"clear","cleared":{"id":"old","revision":1}}`))
	manager.AppendCustomEntry("pi-goal", json.RawMessage(`{"operation":"create","goal":{"id":"new","revision":1,"phase":"active","objective":"Ship the simplified goal card"}}`))
	manager.AppendMessage(&ai.ToolResultMessage{
		ToolCallID: "set", ToolName: "set_goal",
		Content: ai.UserContentList{ai.TextContent{Text: "Goal replaced (id new, revision 1)."}},
		Details: json.RawMessage(`{"goal":{"id":"new","revision":1,"phase":"active"}}`),
	})
	var loaded []coding.SessionEntry
	for _, entry := range manager.GetBranch("") {
		line, err := ai.MarshalJSON(entry)
		if err != nil {
			t.Fatal(err)
		}
		fileEntry, err := coding.UnmarshalFileEntry(string(line))
		if err != nil {
			t.Fatal(err)
		}
		loaded = append(loaded, *fileEntry.Entry)
	}
	renderer.RenderSessionEntries(loaded, false, false)
	got := coding.StripAnsi(strings.Join(renderer.Chat.Render(100), "\n"))
	if strings.Count(got, "Ship the simplified goal card") != 1 || !strings.Contains(got, "Goal replaced ✓") || strings.Contains(got, "Goal cleared") || strings.Contains(got, "Goal created") {
		t.Fatalf("replay duplicated the replacement action:\n%s", got)
	}
}

// A command can change the goal while a tool is pending. Match the successful
// result's exact goal ID, not every lifecycle entry that arrived during the call.
func TestGoalToolCoalescingPreservesCommandCards(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	renderer.EntryRenderer = GoalEntryRenderer
	renderer.Session = &goalCardTranscriptSession{}
	manager := coding.NewSessionManager(t.TempDir(), &coding.SessionManagerOptions{Persist: new(false)})
	manager.AppendMessage(&ai.AssistantMessage{
		Content:    ai.ContentList{ai.ToolCall{ID: "set", Name: "set_goal", Arguments: json.RawMessage(`{"objective":"Tool objective"}`)}},
		StopReason: ai.StopToolUse,
	})
	manager.AppendCustomEntry("pi-goal", json.RawMessage(`{"operation":"create","goal":{"id":"command","revision":1,"phase":"active","objective":"Command objective"}}`))
	manager.AppendCustomEntry("pi-goal", json.RawMessage(`{"operation":"clear","cleared":{"id":"command","revision":1}}`))
	manager.AppendCustomEntry("pi-goal", json.RawMessage(`{"operation":"create","goal":{"id":"tool","revision":1,"phase":"active","objective":"Tool objective"}}`))
	manager.AppendMessage(&ai.ToolResultMessage{
		ToolCallID: "set", ToolName: "set_goal",
		Content: ai.UserContentList{ai.TextContent{Text: "Goal replaced (id tool, revision 1)."}},
		Details: json.RawMessage(`{"goal":{"id":"tool","revision":1,"phase":"active"}}`),
	})
	renderer.RenderSessionEntries(manager.GetBranch(""), false, false)
	got := coding.StripAnsi(strings.Join(renderer.Chat.Render(100), "\n"))
	if strings.Count(got, "Command objective") != 1 || strings.Count(got, "Tool objective") != 1 || strings.Count(got, "Goal created rev 1") != 1 {
		t.Fatalf("coalescing removed a command card or duplicated the tool:\n%s", got)
	}
	// Standalone command entries after a finished tool must also stay visible.
	renderer.AddCustomEntryToChat(&coding.SessionEntry{Type: "custom", CustomType: "pi-goal",
		Data: json.RawMessage(`{"operation":"clear","cleared":{"id":"tool","revision":1}}`),
	})
	if got := coding.StripAnsi(strings.Join(renderer.Chat.Render(100), "\n")); !strings.Contains(got, "Goal cleared") {
		t.Fatalf("standalone clear disappeared:\n%s", got)
	}
}

func TestGoalCreationPreservesIndependentClear(t *testing.T) {
	dispatcher, _, renderer, _ := newEventTestDispatcher(t)
	renderer.EntryRenderer = GoalEntryRenderer
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionToolExecutionStart,
		Agent: &agent.AgentEvent{ToolName: "set_goal", ToolCallID: "set", Args: json.RawMessage(`{"objective":"New objective"}`)},
	})
	for _, entry := range []*coding.SessionEntry{
		{Type: "custom", ID: "clear", CustomType: "pi-goal", Data: json.RawMessage(`{"operation":"clear","cleared":{"id":"old","revision":1}}`)},
		{Type: "custom", ParentID: new("clear"), CustomType: "pi-goal", Data: json.RawMessage(`{"operation":"create","goal":{"id":"new","revision":1,"phase":"active","objective":"New objective"}}`)},
	} {
		dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionEntryAppended, Entry: entry})
	}
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionToolExecutionEnd,
		Agent: &agent.AgentEvent{ToolName: "set_goal", ToolCallID: "set",
			Result: agent.AgentToolResult{
				Content: []ai.Content{ai.TextContent{Text: "Goal created (id new, revision 1)."}},
				Details: json.RawMessage(`{"goal":{"id":"new","revision":1,"phase":"active"}}`),
			}},
	})
	got := coding.StripAnsi(strings.Join(renderer.Chat.Render(100), "\n"))
	if !strings.Contains(got, "Goal cleared") || strings.Count(got, "New objective") != 1 {
		t.Fatalf("creation must retain an independent clear command:\n%s", got)
	}
}

func TestFailedGoalToolPreservesLifecycleCards(t *testing.T) {
	dispatcher, _, renderer, _ := newEventTestDispatcher(t)
	renderer.EntryRenderer = GoalEntryRenderer
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionToolExecutionStart,
		Agent: &agent.AgentEvent{ToolName: "set_goal", ToolCallID: "set", Args: json.RawMessage(`{"objective":"Keep committed state visible"}`)},
	})
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionEntryAppended,
		Entry: &coding.SessionEntry{Type: "custom", CustomType: "pi-goal",
			Data: json.RawMessage(`{"operation":"create","goal":{"id":"new","revision":1,"phase":"active","objective":"Keep committed state visible"}}`)},
	})
	dispatcher.HandleEvent(&coding.SessionEvent{Type: coding.SessionToolExecutionEnd,
		Agent: &agent.AgentEvent{ToolName: "set_goal", ToolCallID: "set", IsError: true,
			Result: agent.AgentToolResult{
				Content: []ai.Content{ai.TextContent{Text: "Result hook failed"}},
				Details: json.RawMessage(`{"goal":{"id":"new","revision":1,"phase":"active"}}`),
			}},
	})
	got := coding.StripAnsi(strings.Join(renderer.Chat.Render(100), "\n"))
	if !strings.Contains(got, "Result hook failed") || !strings.Contains(got, "Goal created rev 1") {
		t.Fatalf("failed tool lost its error or committed lifecycle card:\n%s", got)
	}
}

// Resolve built-in renderers during replay just like the interactive session.
type goalCardTranscriptSession struct{ transcriptTestSession }

func (*goalCardTranscriptSession) GetToolRenderers(name string) *ToolRenderers {
	return WithBuiltInRenderers(name, nil)
}
