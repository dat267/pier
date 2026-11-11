package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// A custom message sent with triggerTurn false while a run is ending reaches
// the transcript once the session settles. Upstream sendCustomMessage
// (core/agent-session.ts, pi v1.5.2 b4afdddf) decides "streaming: defer to
// the turn's end, else append" in one step on its event loop. The port must
// make the same decision under the run-lifecycle lock: deciding on an unlocked
// IsStreaming and queueing afterwards strands the message in the deferred
// queue when the run's final flush runs in between, and nothing delivers it
// until a later run ends.
func TestCustomMessageSentAsARunEndsIsNotStranded(t *testing.T) {
	model := &ai.Model{ID: "fixture", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 128}
	streamFn := func(m *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			time.Sleep(time.Duration(rand.IntN(200)) * time.Microsecond)
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: m.API, Provider: m.Provider, Model: m.ID, StopReason: ai.StopStop,
				Content: ai.ContentList{ai.TextContent{Text: "ok"}},
			}})
		}()
		return stream
	}
	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{Cwd: dir, Sessions: manager, Model: model, StreamFn: streamFn, ConvertToLlm: ConvertToLlm})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()

	const rounds, notesPerRound = 60, 4
	for round := range rounds {
		var group sync.WaitGroup
		group.Go(func() { _ = session.Prompt(context.Background(), fmt.Sprintf("prompt %d", round), nil) })
		for note := range notesPerRound {
			group.Go(func() {
				time.Sleep(time.Duration(rand.IntN(300)) * time.Microsecond)
				content, _ := json.Marshal(map[string]any{"customType": "note", "content": fmt.Sprintf("note %d-%d", round, note), "display": true})
				_ = session.SendMessage(context.Background(), &ai.CustomMessage{Role: "custom", Content: content},
					&SendMessageOptions{TriggerTurn: new(false)})
			})
		}
		group.Wait()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := session.WaitForIdle(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		notes := 0
		for _, message := range session.Agent.State().Messages {
			if custom, ok := message.(*ai.CustomMessage); ok && strings.Contains(string(custom.Content), fmt.Sprintf("note %d-", round)) {
				notes++
			}
		}
		if notes != notesPerRound {
			t.Fatalf("round %d: %d of %d notes reached the transcript after the session settled", round, notes, notesPerRound)
		}
	}
}
