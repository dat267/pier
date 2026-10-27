package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// D224: upstream `~/.pi/agent/extensions/goal/index.ts` queues the round
// prompt at `agent_before_settle`; a mid-round compaction must not discard it.
func TestGoalRoundPromptSurvivesCompactionWithinRound(t *testing.T) {
	manager, dir := newTestSession(t)
	for index := range 4 {
		manager.AppendMessage(&ai.UserMessage{
			Content: ai.StringOrBlocks{Text: fmt.Sprintf("old question %d", index)}, Timestamp: int64(index*2 + 1),
		})
		manager.AppendMessage(&ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
			Content:    ai.ContentList{ai.TextContent{Text: strings.Repeat("old answer ", 12)}},
			StopReason: ai.StopStop, Timestamp: int64(index*2 + 2),
		})
	}

	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 128}
	var session *AgentSession
	var calls int
	var roundContext, afterCompactionContext, subsequentContext []ai.Message
	var compactions atomic.Int64
	var compactionsBeforeThird int64
	streamFn := func(requestModel *ai.Model, transcript ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		calls++
		var message *ai.AssistantMessage
		switch calls {
		case 1:
			create := session.control.Tools["set_goal"].Tool
			_, err := create.Execute("create", json.RawMessage(`{"objective":"continue work"}`), context.Background(), func(agent.AgentToolResult) {})
			if err != nil {
				t.Errorf("create goal tool: %v", err)
			}
			message = &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
				Content: ai.ContentList{ai.TextContent{Text: "starting work"}}, StopReason: ai.StopStop,
				Timestamp: time.Now().UnixMilli(), Usage: ai.Usage{Input: 20, TotalTokens: 20},
			}
		case 2:
			roundContext = append([]ai.Message{}, transcript.Messages...)
			message = &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
				Content:    ai.ContentList{ai.ToolCall{ID: "read-call", Name: "read", Arguments: json.RawMessage(`{}`)}},
				StopReason: ai.StopToolUse, Timestamp: time.Now().UnixMilli(),
				Usage: ai.Usage{Input: 950, TotalTokens: 950},
			}
		case 3:
			afterCompactionContext = append([]ai.Message{}, transcript.Messages...)
			compactionsBeforeThird = compactions.Load()
			message = &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
				Content:    ai.ContentList{ai.ToolCall{ID: "read-call-2", Name: "read", Arguments: json.RawMessage(`{}`)}},
				StopReason: ai.StopToolUse, Timestamp: time.Now().UnixMilli(),
				Usage: ai.Usage{Input: 20, TotalTokens: 20},
			}
		case 4:
			subsequentContext = append([]ai.Message{}, transcript.Messages...)
			snapshot := session.Goal.Snapshot().Goal
			if snapshot == nil {
				t.Error("goal disappeared during compaction")
			} else {
				_, _ = session.control.Tools["set_goal"].Tool.Execute("complete", json.RawMessage(`{"status":"complete"}`), context.Background(), func(agent.AgentToolResult) {})
			}
			fallthrough
		default:
			message = &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
				Content: ai.ContentList{ai.TextContent{Text: "done"}}, StopReason: ai.StopStop,
				Timestamp: time.Now().UnixMilli(), Usage: ai.Usage{Input: 20, TotalTokens: 20},
			}
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: message.StopReason, Message: message})
		}()
		return stream
	}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn,
		InitialMessages: manager.Projection().Messages,
		Tools:           []agent.AgentTool{testTool("read")}, ConvertToLlm: ConvertToLlm, Goal: true,
		Settings: SessionSettings{Compaction: smallCompactionSettings()},
		Control:  &AgentSessionControl{autoCompaction: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	session.CompactionStreamFn = StreamFnFn(summarizingStreamFn(t, &compactions, "old context summarized"))
	if err := session.Prompt(context.Background(), "start", &PromptOptions{}); err != nil {
		t.Fatal(err)
	}
	if compactionsBeforeThird == 0 {
		t.Fatal("no compaction had run before the post-compaction model request")
	}
	if calls < 4 {
		t.Fatalf("model calls = %d, want a request after compaction and one after that", calls)
	}
	goalRoundCount := func(messages []ai.Message) int {
		count := 0
		for _, message := range messages {
			if user, ok := message.(*ai.UserMessage); ok && strings.Contains(contentTextJoinedNoSep(user.Content), "<goal_round>") {
				count++
			}
		}
		return count
	}
	if count := goalRoundCount(roundContext); count != 1 {
		t.Fatalf("goal-round prompt count before compaction = %d, want 1", count)
	}
	if count := goalRoundCount(afterCompactionContext); count != 1 {
		t.Fatalf("goal-round prompt count after compaction = %d, want 1", count)
	}
	if count := goalRoundCount(subsequentContext); count != 1 {
		t.Fatalf("goal-round prompt count on next request = %d, want 1", count)
	}
}
