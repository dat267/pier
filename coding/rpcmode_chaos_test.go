package coding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// rpcRecorder collects the server's output lines.
type rpcRecorder struct {
	mu        sync.Mutex
	pending   []byte
	responses map[string]int
	malformed []string
}

func (r *rpcRecorder) Write(data []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = append(r.pending, data...)
	for {
		end := bytes.IndexByte(r.pending, '\n')
		if end < 0 {
			return len(data), nil
		}
		line := r.pending[:end]
		r.pending = r.pending[end+1:]
		var record struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			r.malformed = append(r.malformed, string(line))
			continue
		}
		if record.Type == "response" && record.ID != "" {
			r.responses[record.ID]++
		}
	}
}

func (r *rpcRecorder) answered() (count int, duplicated []string, malformed []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, times := range r.responses {
		if times > 1 {
			duplicated = append(duplicated, id)
		}
	}
	return len(r.responses), duplicated, append([]string(nil), r.malformed...)
}

// A client that fires commands without waiting for their responses (prompts,
// queueing, aborts, bash, compaction, queries and session replacement, in any
// order) gets exactly one well-formed response per command, and the server
// stops when its input closes. Upstream modes/rpc/rpc-mode.ts (pi v1.5.2
// b4afdddf) answers every command from one event loop; the port spreads them
// over the command owner and its workers.
func TestRPCAnswersEveryCommandOnceUnderAnInterleavedLoad(t *testing.T) {
	model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	streamFn := func(m *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			select {
			case <-options.Ctx.Done():
				aborted := &ai.AssistantMessage{API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopAborted}
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopAborted, Error: aborted})
			case <-time.After(time.Duration(rand.IntN(800)) * time.Microsecond):
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
					API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopStop,
					Content: ai.ContentList{ai.TextContent{Text: "ok"}},
				}})
			}
		}()
		return stream
	}
	build := func(manager *SessionManager) (*AgentSession, error) {
		session, err := NewAgentSession(&SessionConfig{
			Cwd: manager.GetCwd(), Sessions: manager, Model: model, StreamFn: streamFn, ConvertToLlm: ConvertToLlm,
		})
		if err != nil {
			return nil, err
		}
		session.Agent.SetMessages(manager.Projection().Messages)
		return session, nil
	}
	cwd := t.TempDir()
	persist := true
	session, err := build(NewSessionManager(cwd, &SessionManagerOptions{SessionDir: t.TempDir(), Persist: &persist}))
	if err != nil {
		t.Fatal(err)
	}

	commands := []string{
		`"type":"prompt","message":"hello"`,
		`"type":"prompt","message":"queued","streamingBehavior":"followUp"`,
		`"type":"prompt","message":"steered","streamingBehavior":"steer"`,
		`"type":"steer","message":"steer"`,
		`"type":"follow_up","message":"follow"`,
		`"type":"abort"`,
		`"type":"new_session"`,
		`"type":"clone"`,
		`"type":"fork","entryId":"missing"`,
		`"type":"get_state"`,
		`"type":"get_messages"`,
		`"type":"get_session_stats"`,
		`"type":"get_entries"`,
		`"type":"get_tree"`,
		`"type":"get_fork_messages"`,
		`"type":"bash","command":"true"`,
		`"type":"abort_bash"`,
		`"type":"compact"`,
		`"type":"set_session_name","name":"chaos"`,
		`"type":"cycle_thinking_level"`,
		`"type":"clear_queue"`,
		`"type":"set_auto_compaction","enabled":false`,
		`"type":"set_auto_retry","enabled":true`,
		`"type":"abort_retry"`,
		`"type":"set_model","provider":"anthropic","modelId":"fixture"`,
		`"type":"cycle_model"`,
		`"type":"get_available_models"`,
		`"type":"get_available_thinking_levels"`,
		`"type":"set_thinking_level","level":"high"`,
		`"type":"set_steering_mode","mode":"all"`,
		`"type":"set_follow_up_mode","mode":"one-at-a-time"`,
		`"type":"get_last_assistant_text"`,
		`"type":"get_commands"`,
		fmt.Sprintf(`"type":"switch_session","sessionPath":%q`, filepath.Join(t.TempDir(), "other.jsonl")),
		`"type":"export_html"`,
	}
	const total = 200
	input, writer := io.Pipe()
	recorder := &rpcRecorder{responses: map[string]int{}}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunRPCMode(ctx, session, RPCModeOptions{
			Input: input, Output: recorder,
			ReplaceSession: func(_ context.Context, manager *SessionManager) (*AgentSession, error) { return build(manager) },
		})
	}()
	for index := range total {
		line := fmt.Sprintf(`{"id":"c%d",%s}`+"\n", index, commands[rand.IntN(len(commands))])
		if _, err := io.WriteString(writer, line); err != nil {
			t.Fatalf("command %d: %v", index, err)
		}
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		count, _, _ := recorder.answered()
		if count == total {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d commands were answered", count, total)
		}
		time.Sleep(time.Millisecond)
	}
	writer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the server stopped with %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the server did not stop after its input closed")
	}
	if _, duplicated, malformed := recorder.answered(); len(duplicated) > 0 || len(malformed) > 0 {
		t.Fatalf("answered twice: %v; malformed output lines: %v", duplicated, malformed)
	}
}
