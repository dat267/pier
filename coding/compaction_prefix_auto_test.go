package coding

import (
	"context"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// D419: the cached replay is not only the manual `/compact` path. Auto
// compaction runs at the end of the turn that crossed the threshold, so its
// summary request must replay the turn's own prefix too; a capture read after
// the turn's answer is appended still matches, and the answer travels as the
// replay's tail.
func TestAutoCompactionReplaysTheTurnPrefix(t *testing.T) {
	manager := NewSessionManager(t.TempDir(), nil)
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 4000, MaxTokens: 8192}
	answer := func(requestModel *ai.Model, text string, input int64) *ai.AssistantMessageEventStream {
		message := &ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
			Content: ai.ContentList{ai.TextContent{Text: text}}, StopReason: ai.StopStop,
			Timestamp: time.Now().UnixMilli(), Usage: ai.Usage{Input: input, TotalTokens: input},
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: message.StopReason, Message: message})
		}()
		return stream
	}
	var turnPrefix []ai.Message
	var summaryRequests []ai.TranscriptContext
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Sessions: manager, Model: model, ConvertToLlm: ConvertToLlm,
		StreamFn: func(requestModel *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			turnPrefix = append([]ai.Message{}, transcript.Messages...)
			// Above the threshold (4000 - 3200) and small enough that the replay
			// still fits the window, which is the case this path exists for.
			return answer(requestModel, "done", 950)
		},
		Settings: SessionSettings{Compaction: CompactionSettings{Enabled: true, ReserveTokens: 3200, KeepRecentTokens: 1}},
		Control:  &AgentSessionControl{autoCompaction: true, autoRetry: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	session.CompactionStreamFn = func(requestModel *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		summaryRequests = append(summaryRequests, transcript)
		return answer(requestModel, "## Goal\ncontinue", 10)
	}
	if err := session.Prompt(context.Background(), "start", &PromptOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(turnPrefix) == 0 {
		t.Fatal("no turn request was recorded")
	}
	if len(summaryRequests) != 1 {
		t.Fatalf("auto compaction sent %d summary requests, want 1", len(summaryRequests))
	}
	sent := summaryRequests[0].Messages
	if len(sent) < len(turnPrefix)+2 {
		t.Fatalf("summary request sent %d messages, want the %d-message turn prefix, its answer, and the instruction", len(sent), len(turnPrefix))
	}
	for index, message := range turnPrefix {
		if !sameWireMessage(sent[index], message) {
			t.Fatalf("message %d is not the turn's: %s", index, prefixMessageText(sent[index]))
		}
	}
	if tail := prefixMessageText(sent[len(sent)-2]); tail != "done" {
		t.Fatalf("replayed tail = %q, want the turn's answer", tail)
	}
}
