package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func testOpenAIModel() *Model {
	return &Model{
		ID: "gpt-5", Name: "GPT-5", API: APIOpenAICompletions, Provider: "openai",
		BaseURL: "https://api.openai.com/v1", Reasoning: true, Input: []string{"text", "image"},
		Cost:          ModelCost{ModelCostRates: ModelCostRates{Input: 1.25, Output: 10, CacheRead: 0.125, CacheWrite: 0}},
		ContextWindow: 400000, MaxTokens: 128000,
	}
}

func TestStreamOpenAICompletionsTextAndToolFlow(t *testing.T) {
	var chunks []string
	chunkJSON := func(s string) string {
		return "data: " + s + "\n\n"
	}
	chunks = append(chunks,
		chunkJSON(`{"id":"chatcmpl-1","model":"gpt-5","choices":[{"index":0,"delta":{"role":"assistant","content":"He"},"finish_reason":null}]}`),
		chunkJSON(`{"id":"chatcmpl-1","model":"gpt-5","choices":[{"index":0,"delta":{"content":"llo"},"finish_reason":null}]}`),
		chunkJSON(`{"id":"chatcmpl-1","model":"gpt-5","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"bash","arguments":""}}]},"finish_reason":null}]}`),
		chunkJSON(`{"id":"chatcmpl-1","model":"gpt-5","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"cmd\":"}}]},"finish_reason":null}]}`),
		chunkJSON(`{"id":"chatcmpl-1","model":"gpt-5","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"ls\"}"}}]},"finish_reason":null}]}`),
		chunkJSON(`{"id":"chatcmpl-1","model":"gpt-5","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`),
		"data: [DONE]\n\n",
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			_, _ = w.Write([]byte(c))
		}
	}))
	defer server.Close()

	model := testOpenAIModel()
	model.BaseURL = server.URL
	var providerEventCount int
	stream := StreamOpenAICompletions(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &OpenAICompletionsOptions{
		APIKey: "k",
		OnProviderStreamEvent: func(data json.RawMessage, eventModel *Model) {
			if eventModel != model || !json.Valid(data) {
				t.Errorf("provider callback data/model = %s/%p", data, eventModel)
			}
			providerEventCount++
		}})

	var types []string
	var text string
	var finalCall *ToolCall
	for {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		types = append(types, event.Type)
		if event.Type == EventTextDelta {
			text += event.Delta
		}
		if event.Type == EventToolcallEnd {
			finalCall = event.ToolCall
		}
	}
	if text != "Hello" {
		t.Fatalf("text = %q", text)
	}
	if finalCall == nil || finalCall.ID != "call_1" || finalCall.Name != "bash" || string(finalCall.Arguments) != `{"cmd":"ls"}` {
		t.Fatalf("toolCall = %+v", finalCall)
	}
	msg, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != StopToolUse || msg.ResponseID == nil || *msg.ResponseID != "chatcmpl-1" {
		t.Fatalf("message stopReason=%s responseId=%v", msg.StopReason, msg.ResponseID)
	}
	if msg.Usage.Input != 10 || msg.Usage.Output != 5 || msg.Usage.TotalTokens != 15 {
		t.Fatalf("usage = %+v", msg.Usage)
	}
	if providerEventCount != 6 {
		t.Fatalf("provider callback count = %d; want 6", providerEventCount)
	}
	// Ends with text_end and toolcall_end finishing blocks.
	if types[len(types)-1] != EventDone {
		t.Fatalf("last = %s", types[len(types)-1])
	}
}

// Mirrors streamed content accumulation in
// packages/ai/src/api/openai-completions.ts.
func TestStreamOpenAICompletionsTextDeltasAllocateLinearly(t *testing.T) {
	const chunksCount = 512
	delta := strings.Repeat("x", 128)
	chunks := make([]string, 0, chunksCount+2)
	for range chunksCount {
		chunks = append(chunks, "data: {\"choices\":[{\"delta\":{\"content\":\""+delta+"\"},\"finish_reason\":null}]}\n\n")
	}
	chunks = append(chunks, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n", "data: [DONE]\n\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, chunk := range chunks {
			_, _ = w.Write([]byte(chunk))
		}
	}))
	defer server.Close()

	model := testOpenAIModel()
	model.BaseURL = server.URL
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	message, err := StreamOpenAICompletions(model, NormalizeContext(Context{}), &OpenAICompletionsOptions{
		APIKey: "k",
	}).Result(context.Background())
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("stream result: %v", err)
	}
	want := strings.Repeat(delta, chunksCount)
	if len(message.Content) != 1 {
		t.Fatalf("content = %+v, want one block", message.Content)
	}
	if text, ok := message.Content[0].(TextContent); !ok || text.Text != want {
		t.Fatalf("text = %T length %d, want %d bytes", message.Content[0], len(text.Text), len(want))
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("streaming %d bytes of OpenAI Completions text allocated %d bytes", len(want), allocated)
	if allocated > uint64(len(want))*64 {
		t.Fatalf("streaming %d bytes allocated %d bytes, want less than %d", len(want), allocated, len(want)*64)
	}
}

func TestStreamOpenAICompletionsReasoningFieldsAndDetails(t *testing.T) {
	chunks := []string{
		"data: {\"id\":\"c\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"think\"},\"finish_reason\":null}]}",
		"data: {\"id\":\"c\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_details\":[{\"type\":\"reasoning.text\",\"text\":\" more\"}]},\"finish_reason\":null}]}",
		"data: {\"id\":\"c\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}",
		"data: [DONE]",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, c := range chunks {
			_, _ = w.Write([]byte(c + "\n\n"))
		}
	}))
	defer server.Close()

	model := testOpenAIModel()
	model.BaseURL = server.URL
	stream := StreamOpenAICompletions(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &OpenAICompletionsOptions{APIKey: "k"})
	msg, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The reasoning_content delta created a thinking block; reasoning_details
	// appended to it and serialize into the thinking signature at end.
	var found bool
	for _, block := range msg.Content {
		if tc, ok := block.(ThinkingContent); ok {
			found = true
			if tc.Thinking != "think" {
				t.Fatalf("thinking = %q", tc.Thinking)
			}
			if tc.ThinkingSignature == nil || !strings.Contains(*tc.ThinkingSignature, "reasoning.text") {
				t.Fatalf("signature = %v", tc.ThinkingSignature)
			}
		}
	}
	if !found {
		t.Fatalf("no thinking block: %+v", msg.Content)
	}
}

// A proxy such as LiteLLM can aggregate reasoning and the answer into one
// chunk; the thinking block must still precede the text block (#10926).
func TestStreamOpenAICompletionsReasoningPrecedesTextInOneChunk(t *testing.T) {
	chunks := []string{
		"data: {\"id\":\"c\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"think\",\"content\":\"answer\"},\"finish_reason\":null}]}",
		"data: {\"id\":\"c\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}",
		"data: [DONE]",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, c := range chunks {
			_, _ = w.Write([]byte(c + "\n\n"))
		}
	}))
	defer server.Close()

	model := testOpenAIModel()
	model.BaseURL = server.URL
	stream := StreamOpenAICompletions(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &OpenAICompletionsOptions{APIKey: "k"})
	msg, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Content) != 2 {
		t.Fatalf("content = %+v", msg.Content)
	}
	if thinking, ok := msg.Content[0].(ThinkingContent); !ok || thinking.Thinking != "think" {
		t.Fatalf("content[0] = %#v", msg.Content[0])
	}
	if text, ok := msg.Content[1].(TextContent); !ok || text.Text != "answer" {
		t.Fatalf("content[1] = %#v", msg.Content[1])
	}
}

// A finished call whose complete arguments text is malformed keeps no parsed
// prefix; validation reports the raw text (#10944).
func TestStreamOpenAICompletionsMalformedFinalArguments(t *testing.T) {
	chunks := []string{
		`data: {"id":"c","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"bash","arguments":"{\"cmd\":"}}]},"finish_reason":null}]}`,
		`data: {"id":"c","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, c := range chunks {
			_, _ = w.Write([]byte(c + "\n\n"))
		}
	}))
	defer server.Close()

	model := testOpenAIModel()
	model.BaseURL = server.URL
	stream := StreamOpenAICompletions(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &OpenAICompletionsOptions{APIKey: "k"})
	msg, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("content = %+v", msg.Content)
	}
	call, ok := msg.Content[0].(ToolCall)
	if !ok {
		t.Fatalf("content[0] = %#v", msg.Content[0])
	}
	if string(call.Arguments) != `{}` {
		t.Fatalf("arguments = %s; want {}", call.Arguments)
	}
	if !strings.Contains(call.ArgumentsError, "Received arguments:") {
		t.Fatalf("argumentsError = %q", call.ArgumentsError)
	}
}

func TestStreamOpenAICompletionsErrorsAndFinishFallback(t *testing.T) {
	t.Run("missing key", func(t *testing.T) {
		model := testOpenAIModel()
		bomHeader := "\uFEFF"
		stream := StreamOpenAICompletionsSimple(model, NormalizeContext(Context{}), &SimpleStreamOptions{
			Headers: ProviderHeaders{"Authorization": &bomHeader},
		})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopError || *msg.ErrorMessage != "No API key for provider: openai" {
			t.Fatalf("message = %+v", msg)
		}
	})

	t.Run("finish_reason content_filter", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("data: {\"id\":\"c\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"content_filter\"}]}\n\ndata: [DONE]\n\n"))
		}))
		defer server.Close()
		model := testOpenAIModel()
		model.BaseURL = server.URL
		stream := StreamOpenAICompletions(model, NormalizeContext(Context{}), &OpenAICompletionsOptions{APIKey: "k"})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopError || *msg.ErrorMessage != "Provider finish_reason: content_filter" {
			t.Fatalf("message = %+v", msg)
		}
	})

	t.Run("no finish reason with supportsFinishReason errors", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("data: {\"id\":\"c\",\"model\":\"m\",\"choices\":[]}\n\n"))
		}))
		defer server.Close()
		model := testOpenAIModel()
		model.BaseURL = server.URL
		stream := StreamOpenAICompletions(model, NormalizeContext(Context{}), &OpenAICompletionsOptions{APIKey: "k"})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopError || *msg.ErrorMessage != "Stream ended without finish_reason" {
			t.Fatalf("message = %+v", msg)
		}
	})

	t.Run("no finish reason without supportsFinishReason infers stop", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("data: {\"id\":\"c\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"}}]}\n\n"))
		}))
		defer server.Close()
		model := testOpenAIModel()
		model.BaseURL = server.URL
		model.Compat = &ModelCompat{OpenAICompletions: &OpenAICompletionsCompat{SupportsFinishReason: new(false)}}
		stream := StreamOpenAICompletions(model, NormalizeContext(Context{}), &OpenAICompletionsOptions{APIKey: "k"})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopStop {
			t.Fatalf("stopReason = %s", msg.StopReason)
		}
	})
}

// Upstream api/openai-completions.ts ensureToolCallBlock (v1.5.0) only adds a
// later id on the same stream index as another lookup key; the block keeps
// the id it started with.
func TestStreamOpenAICompletionsToolCallKeepsFirstID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range []string{
			`{"id":"c","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"run"}}]}}]}`,
			`{"id":"c","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_2","function":{"arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		} {
			_, _ = w.Write([]byte("data: " + c + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	model := testOpenAIModel()
	model.BaseURL = server.URL
	result, err := StreamOpenAICompletions(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &OpenAICompletionsOptions{APIKey: "k"}).Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content = %+v", result.Content)
	}
	if call := result.Content[0].(ToolCall); call.ID != "call_1" {
		t.Fatalf("tool call id = %q, want call_1", call.ID)
	}
}

// Upstream builds Copilot's per-request headers inside stream()
// (packages/ai/src/api/openai-completions.ts createClient): X-Initiator marks
// the request user- or agent-initiated, Openai-Intent is always sent, and
// Copilot-Vision-Request is added when the transcript carries images. The port
// only applied them on the responses path.
func TestStreamOpenAICompletionsCopilotDynamicHeaders(t *testing.T) {
	chunks := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	tests := []struct {
		name          string
		messages      []Message
		wantInitiator string
		wantVision    string
	}{
		{
			name: "agent initiated",
			messages: []Message{
				&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
				&AssistantMessage{Content: []Content{TextContent{Text: "hello"}}, Timestamp: 2},
			},
			wantInitiator: "agent",
		},
		{
			name: "user initiated with image",
			messages: []Message{
				&UserMessage{Content: StringOrBlocks{Blocks: []Content{
					TextContent{Text: "look"},
					ImageContent{MimeType: "image/png", Data: "AAAA"},
				}}, Timestamp: 1},
			},
			wantInitiator: "user",
			wantVision:    "true",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := map[string]string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for _, name := range []string{"X-Initiator", "Openai-Intent", "Copilot-Vision-Request"} {
					got[name] = r.Header.Get(name)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(chunks))
			}))
			defer server.Close()

			model := testOpenAIModel()
			model.Provider = "github-copilot"
			model.BaseURL = server.URL
			stream := StreamOpenAICompletions(model, NormalizeContext(Context{Messages: tt.messages}),
				&OpenAICompletionsOptions{APIKey: "copilot-token"})
			for {
				if _, ok := stream.Next(context.Background()); !ok {
					break
				}
			}
			msg, err := stream.Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if msg.StopReason != StopStop {
				t.Fatalf("stopReason = %s", msg.StopReason)
			}
			if got["X-Initiator"] != tt.wantInitiator {
				t.Fatalf("X-Initiator = %q, want %q", got["X-Initiator"], tt.wantInitiator)
			}
			if got["Openai-Intent"] != "conversation-edits" {
				t.Fatalf("Openai-Intent = %q, want conversation-edits", got["Openai-Intent"])
			}
			if got["Copilot-Vision-Request"] != tt.wantVision {
				t.Fatalf("Copilot-Vision-Request = %q, want %q", got["Copilot-Vision-Request"], tt.wantVision)
			}
		})
	}
}

// pi v1.5.2 (b4afdddf) api/openai-completions.ts runs the request hooks inside a
// try/catch that reports `error instanceof Error ? error.message : String(error)`.
// The port recovered the panic and formatted it with %v, so a hook that panics
// with an object reported Go syntax instead of "[object Object]".
func TestStreamOpenAICompletionsPayloadHookPanicRendersLikeJavaScript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	model := testOpenAIModel()
	model.BaseURL = server.URL
	stream := StreamOpenAICompletions(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &OpenAICompletionsOptions{
		APIKey: "k",
		OnPayload: func(json.RawMessage, *Model) json.RawMessage {
			panic(map[string]any{"reason": "bad"})
		},
	})
	msg, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if msg.ErrorMessage == nil || !strings.Contains(*msg.ErrorMessage, "[object Object]") {
		t.Fatalf("stream error message = %v, want it to contain %q", msg.ErrorMessage, "[object Object]")
	}
}

// TestOpenAIChunkKeepsItsDeltaOnAWrongTypedSibling covers a streamed chunk. The
// reference skips only a chunk that is not an object (`typeof chunk !==
// "object"`); the SDK has already parsed it, so a wrong-typed member such as a
// numeric `model` is present and harmless. Decoding into the typed struct failed
// the whole chunk and lost its delta content. An array reports false here where
// the reference proceeds: `typeof [] === "object"`, but reading `id` and
// `choices` off an array yields nothing, so both contribute nothing to the
// stream.
func TestOpenAIChunkKeepsItsDeltaOnAWrongTypedSibling(t *testing.T) {
	for _, testCase := range []struct {
		name string
		wire string
		ok   bool
		text string
	}{
		{"well formed", `{"id":"1","choices":[{"delta":{"content":"hi"}}]}`, true, "hi"},
		{"numeric id", `{"id":1,"choices":[{"delta":{"content":"hi"}}]}`, true, "hi"},
		{"numeric model", `{"id":"1","model":2,"choices":[{"delta":{"content":"hi"}}]}`, true, "hi"},
		{"numeric finish_reason", `{"id":"1","choices":[{"finish_reason":2,"delta":{"content":"hi"}}]}`, true, "hi"},
		{"not an object", `[1,2]`, false, ""},
		{"null", `null`, false, ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var chunk openaiChunk
			if got := decodeOpenAIChunkTolerant(json.RawMessage(testCase.wire), &chunk); got != testCase.ok {
				t.Fatalf("ok = %v, want %v", got, testCase.ok)
			}
			text := ""
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != nil {
				text = *chunk.Choices[0].Delta.Content
			}
			if text != testCase.text {
				t.Fatalf("content = %q, want %q", text, testCase.text)
			}
		})
	}
}

// TestStreamOpenAICompletionsKeepsAChunkWithAWrongTypedMember drives a chunk
// carrying a numeric `model` through the real stream. The reference skips only a
// chunk that is not an object, so the SDK-parsed value is harmless and its delta
// still applies; the typed decode failed the chunk and lost the text.
func TestStreamOpenAICompletionsKeepsAChunkWithAWrongTypedMember(t *testing.T) {
	chunks := []string{
		"data: " + `{"id":"c","model":"gpt-5","choices":[{"index":0,"delta":{"content":"He"}}]}` + "\n\n",
		"data: " + `{"id":"c","model":2,"choices":[{"index":0,"delta":{"content":"llo"}}]}` + "\n\n",
		"data: " + `{"id":"c","model":"gpt-5","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n",
		"data:",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			_, _ = w.Write([]byte(c))
		}
	}))
	defer server.Close()

	model := testOpenAIModel()
	model.BaseURL = server.URL
	stream := StreamOpenAICompletions(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &OpenAICompletionsOptions{APIKey: "k"})
	var text string
	for {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		if event.Type == EventTextDelta {
			text += event.Delta
		}
	}
	if text != "Hello" {
		t.Fatalf("text = %q, want the delta from the chunk with the numeric model", text)
	}
}
