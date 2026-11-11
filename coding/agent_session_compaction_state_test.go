package coding

import (
	ctxpkg "context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dat267/pier/ai"
)

// gatedSummarizer blocks every summarization stream call until the test releases
// that call by its ordinal, so two compactions can be interleaved on purpose.
type gatedSummarizer struct {
	calls atomic.Int64
	mu    sync.Mutex
	gates map[int64]chan struct{}
}

func newGatedSummarizer() *gatedSummarizer {
	return &gatedSummarizer{gates: map[int64]chan struct{}{}}
}

func (g *gatedSummarizer) streamFn(model *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	ordinal := g.calls.Add(1)
	gate := make(chan struct{})
	g.mu.Lock()
	g.gates[ordinal] = gate
	g.mu.Unlock()
	stream := ai.NewAssistantMessageEventStream()
	go func() {
		<-gate
		message := &ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: model.Provider, Model: model.ID,
			Content:    ai.ContentList{ai.TextContent{Text: "summary text that is long enough to pass the usability check"}},
			StopReason: ai.StopStop, Usage: ai.Usage{Input: 100, Output: 10, TotalTokens: 110},
		}
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: message})
	}()
	return stream
}

// waitForCall blocks until the summarizer has started its ordinal-th call.
func (g *gatedSummarizer) waitForCall(t *testing.T, ordinal int64) {
	t.Helper()
	for g.calls.Load() < ordinal {
		runtime.Gosched()
	}
}

func (g *gatedSummarizer) release(t *testing.T, ordinal int64) {
	t.Helper()
	g.mu.Lock()
	gate, ok := g.gates[ordinal]
	g.mu.Unlock()
	if !ok {
		t.Fatalf("no stream call %d", ordinal)
	}
	close(gate)
}

// The reported lead was that two manual compactions could clear each other's
// cancel, so IsCompacting went false under a live compaction. The entry point
// serializes them: CompactSession aborts the previous compaction and waits for
// the session to go idle before claiming its own cancel (upstream compact()),
// so only the state of the one live compaction is observable. This pins that
// state: it is true for as long as the compaction runs, and false afterwards.
func TestManualCompactionKeepsCompactingStateUntilItFinishes(t *testing.T) {
	summarizer := newGatedSummarizer()
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	session := compactionTestSession(t, model, summarizer.streamFn, smallCompactionSettings())
	seedBranch(t, session)
	session.control.autoCompaction = false

	done := make(chan error, 1)
	go func() {
		_, err := session.CompactSession(ctxpkg.Background(), "")
		done <- err
	}()
	summarizer.waitForCall(t, 1)
	if !session.IsCompacting() {
		t.Fatal("IsCompacting = false while a compaction is running")
	}

	summarizer.release(t, 1)
	if err := <-done; err != nil {
		t.Fatalf("compaction: %v", err)
	}
	if session.IsCompacting() {
		t.Fatal("IsCompacting = true after the compaction finished")
	}
}
