package durable

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of the built-in tool task.

// Port of "gives tools and hooks the Harness's models" from
// packages/durable/test/harness-tools.test.ts at pi v1.1.0 commit b0114ef5f.
func TestToolTaskExecutesAndAppendsResult(t *testing.T) {
	session, storage := newRootSession(t)
	ctx := context.Background()
	var entryID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		entry, err := tx.AppendEntry(RootConversationID, EntryDraft{
			Kind: AssistantEntry.Kind,
			Model: []ai.Message{&ai.AssistantMessage{
				Content:    ai.ContentList{ai.ToolCall{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{"text":"hi"}`)}},
				StopReason: ai.StopToolUse, Timestamp: 1,
			}},
		})
		if err != nil {
			return err
		}
		entryID = entry.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var taskID Id
	if err := session.Commit(ctx, func(tx *Transaction) error {
		created, err := tx.CreateTask(ToolTask.Definition, dataJSON(ToolTaskInput{Assistant: entryID, CallID: "c1"}),
			TaskOptions{Ownership: TaskOwnership{Kind: TaskOwnedByConversation}, ConversationID: idPointerOf(RootConversationID)})
		if err != nil {
			return err
		}
		taskID = created
		live, err := tx.Doc(LiveDoc.Definition, RootConversationID)
		if err != nil {
			return err
		}
		AssignJSON(live.(map[string]any), "tools", []any{map[string]any{
			"callId": "c1", "name": "echo", "taskId": taskID, "status": ToolSlotPending,
		}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	models := ai.CreateModels(nil)
	seenModels := make(chan *ai.Models, 2)
	echo := ToolRegistration{
		Tool: ai.Tool{Name: "echo", Description: "echoes",
			Parameters: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)},
		Execute: func(args chord.JsonValue, api ToolExecutionApi, ctx chord.Context) (ToolExecutionResult, error) {
			seenModels <- api.Models()
			api.Output([]byte("out"))
			return ToolExecutionResult{Content: []ai.UserContent{ai.TextContent{Text: "done"}}}, nil
		},
	}
	modelsHook := Extension{Name: "models-hook", Hooks: []HookRegistration{Hook(ToolTask, ToolHooks{
		BeforeTool: func(call ai.ToolCall, api HookApi, ctx chord.Context) (*ToolDecision, error) {
			seenModels <- api.Models()
			return nil, nil
		},
	})}}
	scheduler := NewTaskScheduler(TaskSchedulerOptions{
		Session: session, Storage: storage, Registry: NewRegistry(), Models: models, Context: context.Background(),
		Agent: func(conversationID Id, snapshot RegistrySnapshot, ctx chord.Context) (Agent, error) {
			return Agent{ThinkingLevel: "off", Tools: []ToolRegistration{echo}, Extensions: []Extension{modelsHook}}, nil
		},
		Now: func() int64 { return 1000 },
	})
	if err := scheduler.Open(ctx); err != nil {
		t.Fatal(err)
	}
	scheduler.Resume()
	settled, err := scheduler.WaitForTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.State.Status != TaskTerminal || settled.State.Outcome == nil ||
		settled.State.Outcome.Status != OutcomeCompleted {
		t.Fatalf("settled = %+v", settled)
	}
	if got := len(seenModels); got != 2 {
		t.Fatalf("tool and hook received models %d times, want 2", got)
	}
	if first, second := <-seenModels, <-seenModels; first != models || second != models {
		t.Fatalf("tool/hook models = (%p, %p), want (%p, %p)", first, second, models, models)
	}
	// The result entry is a tool result with the tool's content.
	page, err := storage.ScanEntries(ctx, EntryQuery{ConversationID: RootConversationID}, nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range page.Items {
		if entry.Kind != ToolResultEntry.Kind {
			continue
		}
		found = true
		message, ok := entry.Model[0].(*ai.ToolResultMessage)
		if !ok || message.ToolCallID != "c1" || len(message.Content) != 1 ||
			message.Content[0].(ai.TextContent).Text != "done" {
			t.Fatalf("message = %+v", entry.Model[0])
		}
	}
	if !found {
		t.Fatal("no tool result entry")
	}
	// The slot is finished.
	live, ok, err := session.Snapshot(ctx, LiveDoc.Definition, RootConversationID)
	if err != nil || !ok {
		t.Fatalf("live = %v, %v", ok, err)
	}
	tools, _ := live.(map[string]any)["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %+v", tools)
	}
	slot, _ := tools[0].(map[string]any)
	if slot["status"] != ToolSlotDone || slot["entry"] == nil {
		t.Fatalf("slot = %+v", slot)
	}
}
