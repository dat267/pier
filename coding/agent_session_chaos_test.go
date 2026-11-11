package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/offloop"
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

// The same heavy mix with the session's file writes on a write queue, as every
// production host wires them: the file still lists the entries in their order.
func TestAgentSessionConcurrentHeavyAPIWithAWriteQueueKeepsTheFileInOrder(t *testing.T) {
	queue := offloop.New()
	t.Cleanup(queue.Stop)
	runSessionChaosWith(t, heavySessionOps, false, queue)
}

// The heavy mix with a goal set, paused, resumed and cleared at random while
// its rounds run on their own goroutines: the transcript and the file stay in
// step, and the persisted goal transitions replay.
func TestAgentSessionConcurrentGoalCommandsKeepTranscriptAndGoalReplayable(t *testing.T) {
	runSessionChaosGoal(t, func(session *AgentSession) []func(int) {
		return append(heavySessionOps(session), goalCommandOps(session)...)
	}, false, nil, true)
}

func goalCommandOps(session *AgentSession) []func(int) {
	return []func(int){
		func(i int) { session.Goal.HandleCommand(fmt.Sprintf("set objective %d", i)) },
		func(int) { session.Goal.HandleCommand("pause") },
		func(int) { session.Goal.HandleCommand("resume") },
		func(int) { session.Goal.HandleCommand("clear") },
		func(int) { _ = session.Goal.Snapshot(); _ = session.Goal.Pursuing() },
	}
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
		// A real ! command records its result when it finishes, whatever the
		// session is doing by then; an abort cancels any that are running.
		func(i int) {
			_, _ = session.ExecuteBash(context.Background(), fmt.Sprintf("echo bash %d", i), nil, &ExecuteBashOptions{ExcludeFromContext: i%2 == 0})
		},
		func(int) { session.AbortBash() },
	}
}

// chaosTimestamp drops a message's timestamp from the comparison: the agent's
// copy and the session entry are stamped by separate clock reads, as upstream
// stamps them.
var chaosTimestamp = regexp.MustCompile(`,?"timestamp":[0-9]+`)

func runSessionChaos(t *testing.T, extra func(*AgentSession) []func(int), disposeMidway bool) {
	t.Helper()
	runSessionChaosWith(t, extra, disposeMidway, nil)
}

func runSessionChaosWith(t *testing.T, extra func(*AgentSession) []func(int), disposeMidway bool, queue *offloop.Queue) {
	t.Helper()
	runSessionChaosGoal(t, extra, disposeMidway, queue, false)
}

func runSessionChaosGoal(t *testing.T, extra func(*AgentSession) []func(int), disposeMidway bool, queue *offloop.Queue, withGoal bool) {
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
				// One response in three calls the tool, so runs have tool
				// executions and results for the other calls to land between.
				if rand.IntN(3) == 0 {
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopToolUse, Message: &ai.AssistantMessage{
						API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopToolUse,
						Content: ai.ContentList{ai.ToolCall{ID: fmt.Sprintf("call-%d", rand.Uint64()), Name: "read", Arguments: json.RawMessage(`{}`)}},
					}})
					return
				}
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
					API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopStop,
					Content: ai.ContentList{ai.TextContent{Text: "ok"}},
				}})
			}
		}()
		return stream
	}
	manager, dir := newTestSession(t)
	if queue != nil {
		manager.SetWriteQueue(queue)
	}
	slowTool := testTool("read")
	slowTool.Execute = func(_ string, _ json.RawMessage, ctx context.Context, _ func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(rand.IntN(300)) * time.Microsecond):
		}
		return agent.AgentToolResult{Content: ai.ContentList{ai.TextContent{Text: "read"}}}, nil
	}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn, ConvertToLlm: ConvertToLlm,
		Tools: []agent.AgentTool{slowTool}, Goal: withGoal,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Goal rounds run on their own goroutines, as a host's work queue runs
	// them, until the test stops pursuing the goal.
	var goalRounds sync.WaitGroup
	roundCtx, stopRounds := context.WithCancel(context.Background())
	defer stopRounds()
	if withGoal {
		session.Goal.SetStartWork(func(work func(context.Context) error) {
			goalRounds.Go(func() {
				if roundCtx.Err() == nil {
					_ = work(roundCtx)
				}
			})
		})
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
			content, _ := ai.MarshalJSON(fmt.Sprintf("note %d", i))
			_ = session.SendMessage(context.Background(), CreateCustomMessage("note", content, true, nil, 0),
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
	var finished atomic.Int64
	for worker := range workers {
		group.Go(func() {
			defer finished.Add(1)
			for index := range operations {
				ops[rand.IntN(len(ops))](worker*operations + index)
			}
		})
	}
	done := make(chan struct{})
	go func() { group.Wait(); close(done) }()
	if withGoal {
		// A prompt's run keeps continuing while the goal is pursued, by
		// design, and blocks its worker. Once any worker has finished, keep
		// clearing the goal (a late "set" can revive it) until all return.
		go func() {
			for finished.Load() == 0 {
				select {
				case <-done:
					return
				case <-time.After(time.Millisecond):
				}
			}
			for {
				session.Goal.HandleCommand("clear")
				select {
				case <-done:
					return
				case <-time.After(5 * time.Millisecond):
				}
			}
		}()
	}
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
	if withGoal {
		// Stop pursuing the goal, so the session can settle.
		session.Goal.HandleCommand("clear")
		stopRounds()
		roundsDone := make(chan struct{})
		go func() { goalRounds.Wait(); close(roundsDone) }()
		select {
		case <-roundsDone:
		case <-ctx.Done():
			t.Fatal("a goal round did not return")
		}
	}
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
		agentBytes, _ := ai.MarshalJSON(inMemory[index])
		persistedBytes, _ := ai.MarshalJSON(persisted[index])
		agentBytes = chaosTimestamp.ReplaceAll(agentBytes, nil)
		persistedBytes = chaosTimestamp.ReplaceAll(persistedBytes, nil)
		if string(agentBytes) != string(persistedBytes) {
			t.Fatalf("message %d differs\nagent     %s\npersisted %s", index, agentBytes, persistedBytes)
		}
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
	// The file lists the entries in the order the session holds them, whether
	// each write was synchronous, held past the run lock (A483) or queued.
	if err := manager.FlushWrites(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(manager.GetSessionFile())
	if err != nil {
		t.Fatal(err)
	}
	var onDisk []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		var entry struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("session file line %q: %v", line, err)
		}
		if entry.Type != "session" {
			onDisk = append(onDisk, entry.ID)
		}
	}
	entries := manager.GetEntries()
	if len(onDisk) != len(entries) {
		t.Fatalf("the file holds %d entries, the session %d", len(onDisk), len(entries))
	}
	for index, entry := range entries {
		if onDisk[index] != entry.ID {
			t.Fatalf("file entry %d is %s, the session's is %s", index, onDisk[index], entry.ID)
		}
	}
	// The file reopens to the entries the session holds, each with the parent
	// the session linked it to. A navigation that appended nothing moves only
	// the live leaf (upstream reopens at the last entry), so compare the
	// entries and the live leaf's branch rather than the reopened leaf.
	reopened, err := OpenSession(manager.GetSessionFile(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	reopenedEntries := reopened.GetEntries()
	if len(reopenedEntries) != len(entries) {
		t.Fatalf("the reopened file holds %d entries, the session %d", len(reopenedEntries), len(entries))
	}
	for index := range entries {
		got, _ := ai.MarshalJSON(reopenedEntries[index])
		want, _ := ai.MarshalJSON(entries[index])
		if string(got) != string(want) {
			t.Fatalf("reopened entry %d\n got %s\nwant %s", index, got, want)
		}
	}
	if leaf := manager.GetLeafID(); leaf != nil {
		got, _ := ai.MarshalJSON(reopened.GetBranch(*leaf))
		want, _ := ai.MarshalJSON(manager.GetBranch(*leaf))
		if string(got) != string(want) {
			t.Fatalf("the reopened branch to %s differs\n got %s\nwant %s", *leaf, got, want)
		}
	}
	if withGoal {
		// Every persisted goal transition replays: the machine accepted them
		// in the order the file holds them.
		var warnings []string
		var warningsMu sync.Mutex
		session.Goal.SetUIHooks(func(message, level string) {
			if level == "warning" && strings.Contains(message, "corrupt") {
				warningsMu.Lock()
				warnings = append(warnings, message)
				warningsMu.Unlock()
			}
		})
		session.Goal.Start("startup")
		warningsMu.Lock()
		defer warningsMu.Unlock()
		if len(warnings) > 0 {
			t.Fatalf("the persisted goal transitions did not replay: %v", warnings)
		}
	}
	session.Dispose()
}
