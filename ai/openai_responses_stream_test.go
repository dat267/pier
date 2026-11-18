package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// openai-responses tests keyed to upstream (openai-responses.ts +
// -shared.ts): compat resolution, params shape, message conversion, and the
// SSE event loop.

func testResponsesModel() *Model {
	return &Model{
		ID: "gpt-5", Name: "GPT-5", API: APIOpenAIResponses, Provider: "openai",
		BaseURL: "https://api.openai.com/v1", Reasoning: true, Input: []string{"text", "image"},
		Cost:          ModelCost{ModelCostRates: ModelCostRates{Input: 1.25, Output: 10, CacheRead: 0.125}},
		ContextWindow: 400000, MaxTokens: 128000,
	}
}

// Mirrors the text-delta assembly in packages/ai/src/api/openai-responses-shared.ts.
func TestProcessResponsesStreamTextDeltasAvoidPrefixCopies(t *testing.T) {
	const chunks = 512
	delta := strings.Repeat("x", 128)
	text := strings.Repeat(delta, chunks)
	var body strings.Builder
	body.WriteString("data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg\",\"role\":\"assistant\"}}\n\n")
	for range chunks {
		body.WriteString("data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"delta\":\"")
		body.WriteString(delta)
		body.WriteString("\"}\n\n")
	}
	body.WriteString("data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"")
	body.WriteString(text)
	body.WriteString("\"}]}}\n\n")
	body.WriteString("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp\",\"status\":\"completed\"}}\n\n")

	output := &AssistantMessage{}
	stream := NewAssistantMessageEventStream()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	err := ProcessResponsesStream(context.Background(), strings.NewReader(body.String()), output,
		stream, testResponsesModel(), &OpenAIResponsesOptions{}, nil)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("process Responses stream: %v", err)
	}
	if len(output.Content) != 1 {
		t.Fatalf("content = %+v, want one block", output.Content)
	}
	if got, ok := output.Content[0].(TextContent); !ok || got.Text != text {
		t.Fatalf("assembled text = %T length %d, want %d bytes", output.Content[0], len(got.Text), len(text))
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("assembling %d bytes of Responses text deltas allocated %d bytes", len(text), allocated)
	if allocated > uint64(len(text))*64 {
		t.Fatalf("assembling %d bytes allocated %d bytes, want less than %d", len(text), allocated, len(text)*64)
	}
}

func TestProcessResponsesStreamTextAndReasoning(t *testing.T) {
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1"}}`,
		`{"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"thinking..."}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[{"text":"thinking..."}],"encrypted_content":"enc"}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant"}}`,
		`{"type":"response.output_text.delta","output_index":1,"delta":"Hel"}`,
		`{"type":"response.output_text.delta","output_index":1,"delta":"lo"}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Hello"}]}}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":100,"output_tokens":10,"total_tokens":110,"input_tokens_details":{"cached_tokens":40},"output_tokens_details":{"reasoning_tokens":5}}}}`,
		`[DONE]`,
	}
	var chunks []string
	for _, event := range events {
		chunks = append(chunks, "data: "+event+"\n\n")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, chunk := range chunks {
			_, _ = w.Write([]byte(chunk))
		}
	}))
	defer server.Close()

	model := testResponsesModel()
	model.BaseURL = server.URL
	var providerEventTypes []string
	var providerEventModel *Model
	stream := StreamOpenAIResponses(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &OpenAIResponsesOptions{
		APIKey: "k",
		OnProviderStreamEvent: func(data json.RawMessage, eventModel *Model) {
			var event struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(data, &event); err != nil {
				t.Errorf("decode provider event: %v", err)
				return
			}
			providerEventTypes = append(providerEventTypes, event.Type)
			providerEventModel = eventModel
		}})

	var types []string
	var text, thinking string
	for {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		types = append(types, event.Type)
		if event.Type == EventTextDelta {
			text += event.Delta
		}
		if event.Type == EventThinkingDelta {
			thinking += event.Delta
		}
	}
	if text != "Hello" || thinking != "thinking..." {
		t.Fatalf("text=%q thinking=%q", text, thinking)
	}
	want := []string{"start", "thinking_start", "thinking_delta", "thinking_end", "text_start", "text_delta", "text_delta", "text_end", "done"}
	if len(types) != len(want) {
		t.Fatalf("events = %v; want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("events = %v; want %v", types, want)
		}
	}
	wantProviderEvents := []string{
		"response.created", "response.output_item.added", "response.reasoning_summary_text.delta",
		"response.output_item.done", "response.output_item.added", "response.output_text.delta",
		"response.output_text.delta", "response.output_item.done", "response.completed",
	}
	if !reflect.DeepEqual(providerEventTypes, wantProviderEvents) {
		t.Fatalf("provider event types = %v; want %v", providerEventTypes, wantProviderEvents)
	}
	if providerEventModel != model {
		t.Fatalf("provider event model = %p; want %p", providerEventModel, model)
	}
	msg, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != StopStop || msg.ResponseID == nil || *msg.ResponseID != "resp_1" {
		t.Fatalf("message = %+v", msg)
	}
	// Usage: cached tokens split out of input_tokens.
	if msg.Usage.Input != 60 || msg.Usage.Output != 10 || msg.Usage.CacheRead != 40 || msg.Usage.TotalTokens != 110 {
		t.Fatalf("usage = %+v", msg.Usage)
	}
	if msg.Usage.Reasoning == nil || *msg.Usage.Reasoning != 5 {
		t.Fatalf("reasoning = %v", msg.Usage.Reasoning)
	}
	// The reasoning signature carries the encrypted content.
	thinkingBlock := msg.Content[0].(ThinkingContent)
	if thinkingBlock.ThinkingSignature == nil || !strings.Contains(*thinkingBlock.ThinkingSignature, "encrypted_content") {
		t.Fatalf("signature = %v", thinkingBlock.ThinkingSignature)
	}
	// Assistant text block carries a v1 text signature.
	textBlock := msg.Content[1].(TextContent)
	if textBlock.TextSignature == nil || !strings.Contains(*textBlock.TextSignature, `"v":1`) ||
		!strings.Contains(*textBlock.TextSignature, `"phase":"final_answer"`) {
		t.Fatalf("textSignature = %v", textBlock.TextSignature)
	}
}

func TestProcessResponsesStreamFunctionCallAndErrors(t *testing.T) {
	t.Run("function call", func(t *testing.T) {
		events := []string{
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"bash"}}`,
			`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"cmd\":"}`,
			`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"\"ls\"}"}`,
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"bash","arguments":"{\"cmd\":\"ls\"}"}}`,
			`{"type":"response.completed","response":{"id":"resp","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
			`[DONE]`,
		}
		var chunks []string
		for _, event := range events {
			chunks = append(chunks, "data: "+event+"\n\n")
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, chunk := range chunks {
				_, _ = w.Write([]byte(chunk))
			}
		}))
		defer server.Close()
		model := testResponsesModel()
		model.BaseURL = server.URL
		stream := StreamOpenAIResponses(model, NormalizeContext(Context{}),
			&OpenAIResponsesOptions{APIKey: "k"})
		msg, err := stream.Result(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		// Tool calls upgrade the stop reason to toolUse.
		if msg.StopReason != StopToolUse {
			t.Fatalf("stopReason = %s", msg.StopReason)
		}
		call := msg.Content[0].(ToolCall)
		if call.ID != "call_1|fc_1" || call.Name != "bash" || string(call.Arguments) != `{"cmd":"ls"}` {
			t.Fatalf("call = %+v", call)
		}
	})

	t.Run("response.failed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"boom\"}}}\n\n"))
		}))
		defer server.Close()
		model := testResponsesModel()
		model.BaseURL = server.URL
		stream := StreamOpenAIResponses(model, NormalizeContext(Context{}),
			&OpenAIResponsesOptions{APIKey: "k"})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopError || msg.ErrorMessage == nil || !strings.Contains(*msg.ErrorMessage, "server_error: boom") {
			t.Fatalf("message = %+v", msg)
		}
	})

	t.Run("error event with nested error payload", func(t *testing.T) {
		// OpenAI nests first-party error payloads; the SDK flattens them and
		// throws an APIError whose message is the nested error message
		// (openai lib/responses/ResponseStream.ts), so that text is what pi shows.
		want := "The ChatGPT user has reached their Subscription Sharing usage limit. Ask the user to try again after their usage limit resets or use an API key instead."
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`data: {"type":"error","error":{"type":"invalid_request_error","code":"subscription_sharing_usage_limit_exceeded","message":"` + want + `","param":null},"sequence_number":2}` + "\n\n"))
		}))
		defer server.Close()
		model := testResponsesModel()
		model.BaseURL = server.URL
		stream := StreamOpenAIResponses(model, NormalizeContext(Context{}),
			&OpenAIResponsesOptions{APIKey: "k"})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopError || msg.ErrorMessage == nil || *msg.ErrorMessage != want {
			t.Fatalf("message = %+v", msg)
		}
	})

	t.Run("flat error event uses its message", func(t *testing.T) {
		// The SDK falls back to event.message when the payload is not nested.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("data: {\"type\":\"error\",\"code\":\"server_error\",\"message\":\"boom\"}\n\n"))
		}))
		defer server.Close()
		model := testResponsesModel()
		model.BaseURL = server.URL
		stream := StreamOpenAIResponses(model, NormalizeContext(Context{}),
			&OpenAIResponsesOptions{APIKey: "k"})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopError || msg.ErrorMessage == nil || *msg.ErrorMessage != "boom" {
			t.Fatalf("message = %+v", msg)
		}
	})

	t.Run("no terminal event", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\"}}\n\n"))
		}))
		defer server.Close()
		model := testResponsesModel()
		model.BaseURL = server.URL
		stream := StreamOpenAIResponses(model, NormalizeContext(Context{}),
			&OpenAIResponsesOptions{APIKey: "k"})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopError || !strings.Contains(*msg.ErrorMessage, "before a terminal response event") {
			t.Fatalf("message = %+v", msg)
		}
	})

	// The agent must not run tool calls whose output_item.done never arrived
	// (upstream openai-responses-terminal-event.test.ts, 1b2aa0ca0).
	runUnfinished := func(t *testing.T, events []string, want string) {
		var chunks []string
		for _, event := range events {
			chunks = append(chunks, "data: "+event+"\n\n")
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, chunk := range chunks {
				_, _ = w.Write([]byte(chunk))
			}
		}))
		defer server.Close()
		model := testResponsesModel()
		model.BaseURL = server.URL
		stream := StreamOpenAIResponses(model, NormalizeContext(Context{}),
			&OpenAIResponsesOptions{APIKey: "k"})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopError || msg.ErrorMessage == nil || !strings.Contains(*msg.ErrorMessage, want) {
			t.Fatalf("message = %+v", msg)
		}
	}

	t.Run("unfinished tool call", func(t *testing.T) {
		runUnfinished(t, []string{
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"bash","arguments":""}}`,
			`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"command\":\"rm -rf /tmp/build"}`,
			`{"type":"response.completed","response":{"id":"resp_unfinished","status":"completed"}}`,
		}, "unfinished tool call: bash (call_1|fc_1)")
	})

	// llama.cpp omits output_index from every event, so the port's index-keyed
	// slots mix the parallel calls; the unfinished call is rejected either way.
	t.Run("parallel tool calls without output_index", func(t *testing.T) {
		runUnfinished(t, []string{
			`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_a","call_id":"call_a","name":"bash","arguments":""}}`,
			`{"type":"response.function_call_arguments.delta","item_id":"fc_a","delta":"{\"command\":\"echo a\"}"}`,
			`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_b","call_id":"call_b","name":"bash","arguments":""}}`,
			`{"type":"response.function_call_arguments.delta","item_id":"fc_b","delta":"{\"command\":\"echo b\"}"}`,
			`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_a","call_id":"call_a","name":"bash","arguments":"{\"command\":\"echo a\"}"}}`,
			`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_b","call_id":"call_b","name":"bash","arguments":"{\"command\":\"echo b\"}"}}`,
			`{"type":"response.completed","response":{"id":"resp_no_output_index","status":"completed"}}`,
		}, "unfinished tool call: bash (call_a|fc_a)")
	})

	t.Run("incomplete max_output_tokens is length", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"max_output_tokens\"}}}\n\n"))
		}))
		defer server.Close()
		model := testResponsesModel()
		model.BaseURL = server.URL
		stream := StreamOpenAIResponses(model, NormalizeContext(Context{}),
			&OpenAIResponsesOptions{APIKey: "k"})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopLength || msg.RawStopReason == nil || *msg.RawStopReason != "incomplete.max_output_tokens" {
			t.Fatalf("message = %+v", msg)
		}
	})
}
