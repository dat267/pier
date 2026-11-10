package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// Many goroutines drive one session's public API at once (prompts, queueing,
// aborts, bash results, custom messages, settings and reads), as a host's UI
// loop, RPC handler, goal worker and bash completions can. Every call must
// return, the session must settle, and the agent's transcript must match what
// the session persisted. Upstream agent-session.ts (pi v1.5.0 f9a26365)
// serializes these on one event loop; the port must not lose or corrupt state
// when they overlap (D438).
func TestAgentSessionConcurrentAPIKeepsTranscriptAndPersistenceInStep(t *testing.T) {
	runSessionChaos(t, nil, false)
}

// The same, with the session's heavier operations mixed in: compaction, tree
// navigation, tool and thinking changes, next-turn and user messages.
func TestAgentSessionConcurrentHeavyAPIKeepsTranscriptAndPersistenceInStep(t *testing.T) {
	runSessionChaos(t, heavySessionOps, false)
}

// Dispose lands at a random point while every other operation keeps going:
// each call still returns and the session settles. Persistence stops at the
// disposal, so only liveness is checked.
func TestAgentSessionDisposedMidChaosSettles(t *testing.T) {
	runSessionChaos(t, heavySessionOps, true)
}

func heavySessionOps(session *AgentSession) []func(int) {
	return []func(int){
		func(int) { _, _ = session.CompactSession(context.Background(), "") },
		func(int) {
			forks := session.GetUserMessagesForForking()
			if len(forks) > 0 {
				_, _ = session.NavigateTree(context.Background(), forks[rand.IntN(len(forks))].EntryID, NavigateTreeOptions{})
			}
		},
		func(int) { _, _ = session.CycleThinkingLevel(ModelMutationOptions{}) },
		func(int) { session.SetActiveToolsByName([]string{"read", "bash"}[:1+rand.IntN(2)]) },
		func(i int) {
			session.QueueNextTurnMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: fmt.Sprintf("next %d", i)}})
		},
		func(i int) { _ = session.SendUserMessage(context.Background(), fmt.Sprintf("user %d", i), "followUp") },
		func(int) { _ = session.Model(); _ = session.ThinkingLevel(); _ = session.SystemPrompt() },
	}
}

func runSessionChaos(t *testing.T, extra func(*AgentSession) []func(int), disposeMidway bool) {
	t.Helper()
	const workers, operations = 6, 60
	model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	streamFn := func(m *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			select {
			case <-options.Ctx.Done():
				aborted := &ai.AssistantMessage{API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopAborted}
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopAborted, Error: aborted})
			case <-time.After(time.Duration(rand.IntN(500)) * time.Microsecond):
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
					API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopStop,
					Content: ai.ContentList{ai.TextContent{Text: "ok"}},
				}})
			}
		}()
		return stream
	}
	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn, ConvertToLlm: ConvertToLlm})
	if err != nil {
		t.Fatal(err)
	}
	unsubscribe := session.Subscribe(func(*SessionEvent) {})
	defer unsubscribe()

	ops := []func(int){
		func(i int) {
			_ = session.Prompt(context.Background(), fmt.Sprintf("prompt %d", i), &PromptOptions{StreamingBehavior: "followUp"})
		},
		func(i int) {
			session.Steer(&ai.UserMessage{Content: ai.StringOrBlocks{Text: fmt.Sprintf("steer %d", i)}})
		},
		func(i int) {
			session.FollowUp(&ai.UserMessage{Content: ai.StringOrBlocks{Text: fmt.Sprintf("follow %d", i)}})
		},
		func(int) { session.AbortTurnAsync() },
		func(i int) {
			session.RecordBashResult(fmt.Sprintf("echo %d", i), BashResult{Output: "ok", ExitCode: new(0)}, false)
		},
		func(i int) {
			content, _ := json.Marshal(map[string]any{"customType": "note", "content": fmt.Sprintf("note %d", i), "display": true})
			_ = session.SendMessage(context.Background(), &ai.CustomMessage{Role: "custom", Content: content},
				&SendMessageOptions{TriggerTurn: new(false)})
		},
		func(int) { session.SetThinkingLevel(ai.ThinkingLevel([]string{"off", "low", "high"}[rand.IntN(3)])) },
		func(int) { _ = session.GetSessionStats(); _ = session.Messages(); _ = session.GetContextUsage() },
		func(i int) { session.SetSessionName(fmt.Sprintf("name %d", i)) },
		func(int) { session.ClearQueue() },
	}
	if extra != nil {
		ops = append(ops, extra(session)...)
	}
	var group sync.WaitGroup
	if disposeMidway {
		group.Go(func() {
			time.Sleep(time.Duration(rand.IntN(3000)) * time.Microsecond)
			session.Dispose()
		})
	}
	for worker := range workers {
		group.Go(func() {
			for index := range operations {
				ops[rand.IntN(len(ops))](worker*operations + index)
			}
		})
	}
	done := make(chan struct{})
	go func() { group.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("a concurrent session call did not return")
	}

	if disposeMidway {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := session.WaitForIdle(ctx); err != nil {
			t.Fatalf("the disposed session did not settle: %v", err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session.ClearQueue()
	if err := session.WaitForIdle(ctx); err != nil {
		t.Fatalf("the session did not settle: %v", err)
	}
	if session.IsStreaming() {
		t.Fatal("the session still streams after settling")
	}
	inMemory := session.Agent.State().Messages
	persisted := manager.Projection().Messages
	if len(inMemory) != len(persisted) {
		t.Fatalf("agent holds %d messages, the session persisted %d", len(inMemory), len(persisted))
	}
	for index := range inMemory {
		if ai.RoleOf(inMemory[index]) != ai.RoleOf(persisted[index]) {
			roles := func(messages []ai.Message) []string {
				var out []string
				for i := max(0, index-3); i < min(len(messages), index+3); i++ {
					out = append(out, string(ai.RoleOf(messages[i])))
				}
				return out
			}
			t.Fatalf("message %d: agent role %q, persisted role %q\nagent     %v\npersisted %v", index,
				ai.RoleOf(inMemory[index]), ai.RoleOf(persisted[index]), roles(inMemory), roles(persisted))
		}
	}
	session.Dispose()
}
