package coding

import (
	"context"
	"strings"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// summaryRequest is one captured summarization request.
type summaryRequest struct {
	model   *ai.Model
	context ai.TranscriptContext
	options *ai.SimpleStreamOptions
}

// summaryStreamFn answers every request with a checkpoint-shaped reply and
// records what was sent. A lengthStop map makes the numbered request stop on
// the token cap instead.
func summaryStreamFn(requests *[]summaryRequest, lengthStop map[int]bool) StreamFnFn {
	return func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		*requests = append(*requests, summaryRequest{model: model, context: transcript, options: options})
		message := &ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: model.Provider, Model: model.ID,
			Content:    ai.ContentList{ai.TextContent{Text: "## Goal\nship it"}},
			StopReason: ai.StopStop, Timestamp: 1,
			Usage: ai.Usage{Input: 10, CacheRead: 90, TotalTokens: 100},
		}
		if lengthStop[len(*requests)] {
			message.StopReason = ai.StopLength
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: message.StopReason, Message: message})
		}()
		return stream
	}
}

func prefixTestPreparation(t *testing.T) *CompactionPreparation {
	t.Helper()
	return &CompactionPreparation{
		FirstKeptEntryID:    "keep-1",
		MessagesToSummarize: []ai.Message{&ai.UserMessage{Content: ai.StringOrBlocks{Text: "old question"}}},
		TokensBefore:        1234,
		FileOps:             CreateFileOps(),
	}
}

func prefixUserTurns(texts ...string) []ai.Message {
	messages := make([]ai.Message, 0, len(texts))
	for _, text := range texts {
		messages = append(messages, &ai.UserMessage{Content: ai.StringOrBlocks{Text: text}})
	}
	return messages
}

// A provider caches the request prefix, so summarizing from the session's own
// last request turns the compaction summary into a cache read instead of a cold
// prefill of the whole conversation (D419).
func TestCompactSummarizesFromTheCapturedPrefix(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 200000, Reasoning: true}
	turn := prefixUserTurns("first", "second", "third")
	var requests []summaryRequest
	options := CompactionOptions{
		Model: model, Ctx: context.Background(), SessionID: "sess-1",
		ThinkingLevel: ai.ThinkHigh,
		StreamFn:      summaryStreamFn(&requests, nil),
		Prefix:        &CompactionPrefix{Model: model, Messages: turn, Reasoning: ai.ThinkHigh},
	}

	result, err := Compact(prefixTestPreparation(t), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "## Goal\nship it" {
		t.Fatalf("summary = %q", result.Summary)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want one", len(requests))
	}
	sent := requests[0]
	if len(sent.context.Messages) != len(turn)+1 {
		t.Fatalf("sent %d messages, want the %d captured plus the instruction", len(sent.context.Messages), len(turn))
	}
	for i, message := range turn {
		if sent.context.Messages[i] != message {
			t.Fatalf("message %d is not the captured one", i)
		}
	}
	instruction, ok := sent.context.Messages[len(turn)].(*ai.UserMessage)
	if !ok || !strings.Contains(contentTextJoinedNoSep(instruction.Content), "structured context checkpoint") {
		t.Fatalf("last message is not the summary instruction: %#v", sent.context.Messages[len(turn)])
	}
	if sent.options.CacheRetention == ai.CacheRetentionNone {
		t.Fatal("the summary request must read the conversation from the prompt cache")
	}
	if sent.options.SessionID != "sess-1" {
		t.Fatalf("session id = %q, want the conversation's", sent.options.SessionID)
	}
	if sent.options.Reasoning != ai.ThinkHigh {
		t.Fatalf("reasoning = %q, want the captured thinking level", sent.options.Reasoning)
	}
	if result.Usage == nil || result.Usage.CacheRead != 90 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

// The captured request belongs to one model's wire format and prefix cache, so
// a compaction for another model must not replay it.
func TestCompactFallsBackWhenThePrefixModelDiffers(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 200000}
	other := &ai.Model{ID: "m2", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 200000}
	var requests []summaryRequest
	options := CompactionOptions{
		Model: model, Ctx: context.Background(), StreamFn: summaryStreamFn(&requests, nil),
		Prefix: &CompactionPrefix{Model: other, Messages: prefixUserTurns("first"), Reasoning: ai.ThinkOff},
	}

	if _, err := Compact(prefixTestPreparation(t), options); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
	if !isStockSummarizationRequest(requests[0].context) {
		t.Fatal("a prefix from another model must fall back to the serialized prompt")
	}
}

// A prefix summary that runs out of answer room cannot become a checkpoint, so
// the serialized path still produces one (D419 keeps D264's escalation).
func TestCompactFallsBackWhenThePrefixSummaryDoesNotFit(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 200000}
	var requests []summaryRequest
	options := CompactionOptions{
		Model: model, Ctx: context.Background(), StreamFn: summaryStreamFn(&requests, map[int]bool{1: true}),
		Prefix: &CompactionPrefix{Model: model, Messages: prefixUserTurns("first", "second"), Reasoning: ai.ThinkOff},
	}

	result, err := Compact(prefixTestPreparation(t), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "## Goal\nship it" {
		t.Fatalf("summary = %q", result.Summary)
	}
	if len(requests) < 2 {
		t.Fatalf("requests = %d, want the prefix attempt and the fallback", len(requests))
	}
	if !isStockSummarizationRequest(requests[len(requests)-1].context) {
		t.Fatal("the fallback must use the serialized conversation prompt")
	}
}

// isStockSummarizationRequest reports whether a request is the serialized
// summarization prompt: its only message carries the transcript plus the
// summarization system prompt.
func isStockSummarizationRequest(context ai.TranscriptContext) bool {
	if len(context.Messages) == 0 {
		return false
	}
	system, ok := context.Messages[0].(*ai.SystemMessage)
	return ok && contentTextJoinedNoSep(system.Content) == SummarizationSystemPrompt
}

// D419: the summary request reuses the turn's own prefix, so the instruction is
// the only new content. The stock path appends nothing to a real request.
func TestCompactPrefixInstructionFollowsTheConversation(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 200000}
	var requests []summaryRequest
	options := CompactionOptions{
		Model: model, Ctx: context.Background(), StreamFn: summaryStreamFn(&requests, nil),
		Prefix: &CompactionPrefix{Model: model, Messages: prefixUserTurns("first", "second"), Reasoning: ai.ThinkOff},
	}
	preparation := prefixTestPreparation(t)
	preparation.PreviousSummary = "## Goal\nearlier checkpoint"

	if _, err := Compact(preparation, options); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
	messages := requests[0].context.Messages
	if len(messages) != 4 {
		t.Fatalf("sent %d messages, want 2 captured plus the checkpoint and the update instruction", len(messages))
	}
	checkpoint, ok := messages[2].(*ai.UserMessage)
	if !ok || !strings.Contains(contentTextJoinedNoSep(checkpoint.Content), "<previous-summary>") {
		t.Fatalf("the replayed prefix must be followed by the checkpoint: %#v", messages[2])
	}
	update, ok := messages[3].(*ai.UserMessage)
	if !ok || !strings.Contains(contentTextJoinedNoSep(update.Content), "NEW conversation messages") {
		t.Fatalf("the last message must be the update instruction: %#v", messages[3])
	}
}

var _ = agent.AgentTool{}

// An overflow recovery has no room left for the instruction, so the replay is
// skipped instead of sending a request the window cannot hold (D419 keeps the
// stock path reachable exactly when the context is the problem).
func TestCompactSkipsThePrefixWhenTheWindowIsFull(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 2000}
	var requests []summaryRequest
	options := CompactionOptions{
		Model: model, Ctx: context.Background(), StreamFn: summaryStreamFn(&requests, nil),
		Prefix: &CompactionPrefix{Model: model, Messages: prefixUserTurns("first", "second"), Reasoning: ai.ThinkOff},
	}
	preparation := prefixTestPreparation(t)
	preparation.TokensBefore = 2000

	if _, err := Compact(preparation, options); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
	if !isStockSummarizationRequest(requests[0].context) {
		t.Fatal("a full window must fall back to the serialized prompt")
	}
}

// The cache_control marker a request carries depends on its retention, so a
// replay that resolves a different retention than the turn did changes the
// prefix and loses the cache read it exists for (D419).
func TestCompactReplaysTheCapturedCacheRetention(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 200000}
	cases := []struct {
		name     string
		captured ai.CacheRetention
		envLong  bool
		want     ai.CacheRetention
	}{
		{name: "unset turn follows the provider default", want: ai.CacheRetentionShort},
		{name: "the turn asked for a long cache", captured: ai.CacheRetentionLong, want: ai.CacheRetentionLong},
		{name: "the environment asks for a long cache", envLong: true, want: ai.CacheRetentionLong},
		{name: "the turn disabled caching", captured: ai.CacheRetentionNone, want: ai.CacheRetentionNone},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.envLong {
				t.Setenv("PI_CACHE_RETENTION", "long")
			} else {
				t.Setenv("PI_CACHE_RETENTION", "")
			}
			var requests []summaryRequest
			options := CompactionOptions{
				Model: model, Ctx: context.Background(), StreamFn: summaryStreamFn(&requests, nil),
				Prefix: &CompactionPrefix{
					Model: model, Messages: prefixUserTurns("first"), CacheRetention: testCase.captured,
				},
			}
			if _, err := Compact(prefixTestPreparation(t), options); err != nil {
				t.Fatal(err)
			}
			if len(requests) != 1 {
				t.Fatalf("requests = %d", len(requests))
			}
			if got := requests[0].options.CacheRetention; got != testCase.want {
				t.Fatalf("replay retention = %q, want %q", got, testCase.want)
			}
		})
	}
}
