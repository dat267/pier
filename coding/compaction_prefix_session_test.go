package coding

import (
	"context"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// D419: the session remembers the request it last sent, so a compaction
// summarizes from that request instead of reserializing the conversation.
func TestSessionCompactionReplaysTheLastTurnPrefix(t *testing.T) {
	dir := t.TempDir()
	manager := NewSessionManager(dir, nil)
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 200000, MaxTokens: 8192}
	answer := func(requestModel *ai.Model, text string) *ai.AssistantMessageEventStream {
		message := &ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
			Content: ai.ContentList{ai.TextContent{Text: text}}, StopReason: ai.StopStop,
			Timestamp: time.Now().UnixMilli(), Usage: ai.Usage{Input: 10, TotalTokens: 10},
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: message.StopReason, Message: message})
		}()
		return stream
	}
	var turnPrefix []ai.Message
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Model: model,
		StreamFn: func(requestModel *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			turnPrefix = append([]ai.Message{}, transcript.Messages...)
			return answer(requestModel, "done")
		},
		ConvertToLlm: ConvertToLlm, Settings: SessionSettings{Compaction: smallCompactionSettings()},
		Control: &AgentSessionControl{autoCompaction: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Prompt(context.Background(), "start", &PromptOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(turnPrefix) == 0 {
		t.Fatal("no turn request was recorded")
	}

	var summaryRequests []ai.TranscriptContext
	session.CompactionStreamFn = func(requestModel *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		summaryRequests = append(summaryRequests, transcript)
		return answer(requestModel, "## Goal\ncontinue")
	}
	if _, err := session.CompactSession(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(summaryRequests) != 1 {
		t.Fatalf("summary requests = %d", len(summaryRequests))
	}
	sent := summaryRequests[0].Messages
	if len(sent) != len(turnPrefix)+1 {
		t.Fatalf("summary request sent %d messages, want the %d-message turn prefix plus the instruction", len(sent), len(turnPrefix))
	}
	for i, message := range turnPrefix {
		if sent[i] != message {
			t.Fatalf("message %d is not the last turn's", i)
		}
	}
}
