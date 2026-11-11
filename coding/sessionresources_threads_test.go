package coding

import (
	ctxpkg "context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.5.2 (b4afdddf) api/anthropic-messages.ts registers its
// thread-state cleanup with session-resources.ts. The port cannot register it
// from ai, so CleanupSessionResources clears it here where the session's other
// resources are released.
func TestCleanupSessionResourcesForgetsMessageThreads(t *testing.T) {
	model := &ai.Model{
		ID: "claude-test", API: ai.APIAnthropicMessages, Provider: "anthropic", Input: []string{"text"},
		ContextWindow: 200000, MaxTokens: 100,
		Compat: &ai.ModelCompat{AnthropicMessages: &ai.AnthropicMessagesCompat{SupportsMessageThreads: new(true)}},
	}
	var (
		mu      sync.Mutex
		threads []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Thread *struct {
				Type string `json:"type"`
			} `json:"thread"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		threadType := ""
		if body.Thread != nil {
			threadType = body.Thread.Type
		}
		mu.Lock()
		threads = append(threads, threadType)
		id := fmt.Sprintf("msg_%d", len(threads))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":%q,\"model\":%q,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n", id, model.ID)
		fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n")
		fmt.Fprint(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n")
		fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()
	model.BaseURL = server.URL
	t.Cleanup(func() { ai.ResetAnthropicMessageThreads("") })

	turn := func(messages []ai.Message) *ai.AssistantMessage {
		zero := 0
		options := &ai.AnthropicOptions{StreamOptions: ai.StreamOptions{
			APIKey: "test-key", SessionID: "cleanup-threads", MaxRetries: &zero,
		}}
		resolved := ai.NormalizeContext(ai.Context{SystemPrompt: new("sys"), Messages: messages})
		message, err := ai.StreamAnthropic(model, resolved, options).Result(ctxpkg.Background())
		if err != nil {
			t.Fatal(err)
		}
		return message
	}
	user := func(text string) ai.Message {
		return &ai.UserMessage{Content: ai.StringOrBlocks{Text: text}, Timestamp: 1}
	}

	messages := []ai.Message{user("first")}
	messages = append(messages, turn(messages))
	messages = append(messages, user("second"))
	messages = append(messages, turn(messages))
	mu.Lock()
	sent := append([]string(nil), threads...)
	mu.Unlock()
	if want := []string{"create", "continue"}; !slices.Equal(sent, want) {
		t.Fatalf("thread types = %v, want %v", sent, want)
	}

	if err := CleanupSessionResources("cleanup-threads"); err != nil {
		t.Fatal(err)
	}
	messages = append(messages, user("third"))
	turn(messages)
	mu.Lock()
	sent = append([]string(nil), threads...)
	mu.Unlock()
	if got := sent[len(sent)-1]; got != "create" {
		t.Fatalf("thread after cleanup = %q, want a new thread", got)
	}
}
