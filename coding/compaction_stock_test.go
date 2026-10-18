package coding

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// TestCompactUsesStockSummarizer follows v1.2.0
// core/compaction/compaction.ts compact: the built-in compaction extension is
// not run, so the stock prompt is sent and the caller's auth
// headers reach the summarizer request.
func TestCompactUsesStockSummarizer(t *testing.T) {
	var prompt string
	var seenHeaders ai.ProviderHeaders
	options := CompactionOptions{
		Model:   &ai.Model{ID: "mock", API: "openai-responses", Provider: "openai", MaxTokens: 8192},
		Headers: map[string]string{"x-test-auth": "1"},
		StreamFn: func(model *ai.Model, context ai.TranscriptContext, opts *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			seenHeaders = opts.Headers
			for _, message := range context.Messages {
				user, ok := message.(*ai.UserMessage)
				if !ok {
					continue
				}
				for _, block := range user.Content.Blocks {
					if text, ok := block.(ai.TextContent); ok {
						prompt += text.Text
					}
				}
			}
			stream := ai.NewAssistantMessageEventStream()
			msg := &ai.AssistantMessage{
				Content: ai.ContentList{ai.TextContent{Text: "## Goal\nstock summary"}},
				API:     model.API, Provider: model.Provider, Model: model.ID,
				StopReason: ai.StopStop, Timestamp: 1,
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: msg})
			return stream
		},
	}
	preparation := &CompactionPreparation{
		FirstKeptEntryID:    "entry-1",
		MessagesToSummarize: []ai.Message{createUserMessage(strings.Repeat("u", 200))},
		TokensBefore:        1000,
		FileOps:             CreateFileOps(),
		Settings:            CompactionSettings{ReserveTokens: 16384, KeepRecentTokens: 20000},
	}

	result, err := Compact(preparation, options)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !strings.HasPrefix(result.Summary, "## Goal") {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(prompt, summarizationPrompt) {
		t.Fatal("the stock summarization prompt was not sent")
	}
	if strings.Contains(prompt, "HARD BUDGET") {
		t.Fatal("the built-in better-compact prompt was sent")
	}
	if seenHeaders["x-test-auth"] == nil || *seenHeaders["x-test-auth"] != "1" {
		t.Fatalf("auth headers did not reach the summarizer: %v", seenHeaders)
	}
}
