package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Streaming tests replicating the Anthropic SSE wire format (the same shapes
// upstream's mock-server tests replay).

func TestStreamAnthropicTextFlow(t *testing.T) {
	model := testAnthropicModel()
	model.BaseURL = "placeholder"
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-opus-4-5\",\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hel\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"lo\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	var capturedPath, capturedKey, capturedVersion, capturedBody string
	server := sseServer(t, events, func(r *http.Request, body []byte) {
		capturedPath = r.URL.String()
		capturedKey = r.Header.Get("X-Api-Key")
		capturedVersion = r.Header.Get("anthropic-version")
		capturedBody = string(body)
	})
	model.BaseURL = server.URL
	defer server.Close()

	var providerEventCount int
	stream := StreamAnthropic(model, NormalizeContext(Context{
		SystemPrompt: new("sys"),
		Messages:     []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &AnthropicOptions{
		APIKey: "test-key",
		OnProviderStreamEvent: func(data json.RawMessage, eventModel *Model) {
			if eventModel != model || !json.Valid(data) {
				t.Errorf("provider callback data/model = %s/%p", data, eventModel)
			}
			providerEventCount++
		}})

	var types []string
	var text string
	for {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		types = append(types, event.Type)
		if event.Type == EventTextDelta {
			text += event.Delta
		}
	}
	want := []string{"start", "text_start", "text_delta", "text_delta", "text_end", "done"}
	if len(types) != len(want) {
		t.Fatalf("events = %v; want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("events = %v; want %v", types, want)
		}
	}
	if text != "Hello" {
		t.Fatalf("text = %q", text)
	}

	msg, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != StopStop || msg.ResponseID == nil || *msg.ResponseID != "msg_1" {
		t.Fatalf("message = %+v", msg)
	}
	if providerEventCount != 7 {
		t.Fatalf("provider callback count = %d; want 7", providerEventCount)
	}
	if msg.Usage.Input != 10 || msg.Usage.Output != 5 || msg.Usage.TotalTokens != 15 {
		t.Fatalf("usage = %+v", msg.Usage)
	}
	// Cost computed from catalog rates: input 10 * $5/M, output 5 * $25/M.
	if msg.Usage.Cost.Input != (5.0/1000000.0)*10.0 || msg.Usage.Cost.Output != (25.0/1000000.0)*5.0 {
		t.Fatalf("cost = %+v", msg.Usage.Cost)
	}

	// Request wire shape: SDK-compatible URL, headers, body keys.
	if capturedPath != "/v1/messages?beta=true" {
		t.Fatalf("path = %s", capturedPath)
	}
	if capturedKey != "test-key" {
		t.Fatalf("x-api-key = %q", capturedKey)
	}
	if capturedVersion != "2023-06-01" {
		t.Fatalf("anthropic-version = %q", capturedVersion)
	}
	var bodyMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(capturedBody), &bodyMap); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"model", "messages", "max_tokens", "stream", "system"} {
		if _, ok := bodyMap[key]; !ok {
			t.Fatalf("body missing %q: %s", key, capturedBody)
		}
	}
	if string(bodyMap["stream"]) != "true" {
		t.Fatalf("stream = %s", bodyMap["stream"])
	}
}

// Mirrors streaming text_block_delta assembly in
// packages/ai/src/api/anthropic-messages.ts.
func TestStreamAnthropicTextDeltasAllocateLinearly(t *testing.T) {
	const chunks = 512
	delta := strings.Repeat("x", 128)
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\",\"model\":\"claude-opus-4-5\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
	}
	deltaEvent := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + delta + "\"}}\n\n"
	for range chunks {
		events = append(events, deltaEvent)
	}
	events = append(events,
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")

	model := testAnthropicModel()
	server := sseServer(t, events, nil)
	model.BaseURL = server.URL
	defer server.Close()
	transcript := NormalizeContext(Context{Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}}})
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	message, err := StreamAnthropic(model, transcript, &AnthropicOptions{APIKey: "test-key"}).Result(context.Background())
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("stream result: %v", err)
	}
	want := strings.Repeat(delta, chunks)
	if len(message.Content) != 1 {
		t.Fatalf("content = %+v, want one text block", message.Content)
	}
	if text, ok := message.Content[0].(TextContent); !ok || text.Text != want {
		t.Fatalf("text = %T length %d, want %d bytes", message.Content[0], len(text.Text), len(want))
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("streaming %d bytes of Anthropic text allocated %d bytes", len(want), allocated)
	if allocated > uint64(len(want))*64 {
		t.Fatalf("streaming %d bytes allocated %d bytes, want less than %d", len(want), allocated, len(want)*64)
	}
}

func TestStreamAnthropicToolUseWithPartialJSON(t *testing.T) {
	model := testAnthropicModel()
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_2\",\"model\":\"claude-opus-4-5\",\"usage\":{\"input_tokens\":8,\"output_tokens\":1}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"bash\",\"input\":{}}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"cmd\\\":\\\"ls\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\" -la\\\"}\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":9}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	server := sseServer(t, events, nil)
	model.BaseURL = server.URL
	defer server.Close()

	stream := StreamAnthropic(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "list"}, Timestamp: 1}},
	}), &AnthropicOptions{APIKey: "k"})

	var deltas []string
	var finalCall *ToolCall
	for {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		if event.Type == EventToolcallDelta {
			deltas = append(deltas, event.Delta)
		}
		if event.Type == EventToolcallEnd {
			finalCall = event.ToolCall
		}
	}
	if len(deltas) != 2 {
		t.Fatalf("deltas = %q", deltas)
	}
	msg, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != StopToolUse {
		t.Fatalf("stopReason = %s", msg.StopReason)
	}
	if finalCall == nil || finalCall.Name != "bash" || string(finalCall.Arguments) != `{"cmd":"ls -la"}` {
		t.Fatalf("toolCall = %+v", finalCall)
	}
	if rawStop := msg.RawStopReason; rawStop == nil || *rawStop != "tool_use" {
		t.Fatalf("rawStopReason = %v", msg.RawStopReason)
	}
}

func TestStreamAnthropicThinkingAndRedacted(t *testing.T) {
	model := testAnthropicModel()
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"claude-opus-4-5\",\"usage\":{\"input_tokens\":5,\"output_tokens\":1}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"hmm\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig123\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"redacted_thinking\",\"data\":\"enc-payload\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	server := sseServer(t, events, nil)
	model.BaseURL = server.URL
	defer server.Close()

	stream := StreamAnthropic(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &AnthropicOptions{APIKey: "k"})

	var events0 []string
	for {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		events0 = append(events0, event.Type)
	}
	want := []string{"start", "thinking_start", "thinking_delta", "thinking_end", "thinking_start", "thinking_end", "done"}
	if len(events0) != len(want) {
		t.Fatalf("events = %v; want %v", events0, want)
	}
	msg, _ := stream.Result(context.Background())
	thinking := msg.Content[0].(ThinkingContent)
	if thinking.Thinking != "hmm" || thinking.ThinkingSignature == nil || *thinking.ThinkingSignature != "sig123" {
		t.Fatalf("thinking = %+v", thinking)
	}
	redacted := msg.Content[1].(ThinkingContent)
	if !redacted.Redacted || *redacted.ThinkingSignature != "enc-payload" || redacted.Thinking != "[Reasoning redacted]" {
		t.Fatalf("redacted = %+v", redacted)
	}
}

func TestStreamAnthropicAbort(t *testing.T) {
	model := testAnthropicModel()
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"claude-opus-4-5\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-block
	}))
	model.BaseURL = server.URL
	defer server.Close()
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := StreamAnthropic(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &AnthropicOptions{APIKey: "k", Ctx: ctx})

	// Wait for start, then abort.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		if event.Type == EventStart {
			cancel()
		}
	}
	msg, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != StopAborted {
		t.Fatalf("stopReason = %s; want aborted", msg.StopReason)
	}
	_ = msg.ErrorMessage
}

func TestStreamAnthropicSimpleMapping(t *testing.T) {
	// streamSimple: no reasoning → thinking disabled.
	model := testAnthropicModel()
	var captured map[string]json.RawMessage
	server := sseServer(t, []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"claude-opus-4-5\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, func(r *http.Request, body []byte) {
		captured = map[string]json.RawMessage{}
		json.Unmarshal(body, &captured)
	})
	model.BaseURL = server.URL
	defer server.Close()

	stream := StreamAnthropicSimple(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &SimpleStreamOptions{APIKey: "k"})
	msg, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != StopStop {
		t.Fatalf("stopReason = %s", msg.StopReason)
	}
	if thinking, ok := captured["thinking"]; !ok || !strings.Contains(string(thinking), `"type":"disabled"`) {
		t.Fatalf("thinking = %s", captured["thinking"])
	}

	// Reasoning low → budget-based thinking with 2048 budget.
	stream = StreamAnthropicSimple(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &SimpleStreamOptions{APIKey: "k", Reasoning: ThinkLow})
	if _, err := stream.Result(context.Background()); err != nil {
		t.Fatal(err)
	}
	if thinking, ok := captured["thinking"]; !ok || !strings.Contains(string(thinking), `"budget_tokens":2048`) {
		t.Fatalf("thinking = %s", captured["thinking"])
	}
	// max_tokens = model cap (64000).
	if string(captured["max_tokens"]) != "64000" {
		t.Fatalf("max_tokens = %s", captured["max_tokens"])
	}
}

// Upstream api/anthropic-messages.ts (v1.5.0) keeps an openBlocks map keyed by
// the Anthropic block index: a stop closes the entry, so later deltas for it
// are ignored, and a new start on the same index replaces the entry. Deltas to
// a tool block after another block started must not reuse copied stream state.
func TestStreamAnthropicOpenBlocksFollowBlockIndex(t *testing.T) {
	ev := func(name, data string) string { return "event: " + name + "\ndata: " + data + "\n\n" }
	events := []string{
		ev("message_start", `{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-4-5","usage":{"input_tokens":1,"output_tokens":1}}}`),
		ev("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"a"}}`),
		ev("content_block_stop", `{"type":"content_block_stop","index":0}`),
		ev("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"late"}}`),
		ev("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}}`),
		ev("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"z\":"}}`),
		ev("content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"thinking","thinking":""}}`),
		ev("content_block_start", `{"type":"content_block_start","index":3,"content_block":{"type":"text","text":""}}`),
		ev("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"q\"}"}}`),
		ev("content_block_stop", `{"type":"content_block_stop","index":1}`),
		ev("content_block_start", `{"type":"content_block_start","index":3,"content_block":{"type":"text","text":"b"}}`),
		ev("content_block_delta", `{"type":"content_block_delta","index":3,"delta":{"type":"text_delta","text":"c"}}`),
		ev("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`),
		ev("message_stop", `{"type":"message_stop"}`),
	}
	server := sseServer(t, events, nil)
	defer server.Close()
	model := testAnthropicModel()
	model.BaseURL = server.URL
	result, err := StreamAnthropic(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &AnthropicOptions{APIKey: "k"}).Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != StopStop {
		t.Fatalf("stop = %s error = %v", result.StopReason, deref(result.ErrorMessage))
	}
	content, err := MarshalJSON(result.Content)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"type":"text","text":"a"},{"type":"toolCall","id":"toolu_1","name":"lookup","arguments":{"z":"q"}},{"type":"thinking","thinking":"","thinkingSignature":""},{"type":"text","text":""},{"type":"text","text":"bc"}]`
	if string(content) != want {
		t.Fatalf("content = %s\nwant %s", content, want)
	}
}
