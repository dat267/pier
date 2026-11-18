package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 f9a26365 api/openai-responses-shared.ts processes
// function_call_arguments.done with ToolCallArgumentsStream.replace, so a
// later output_item.done without arguments finishes the provider's final
// replacement, never a stale streamed prefix.
func TestResponsesKeepsFinalArgumentReplacement(t *testing.T) {
	for _, final := range []string{`{"command":"new"}`, `{"command":`} {
		t.Run(final, func(t *testing.T) {
			encoded, err := MarshalJSON(final)
			if err != nil {
				t.Fatal(err)
			}
			body := strings.Join([]string{
				`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc","call_id":"call","name":"echo","arguments":""}}`,
				`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"command\":\"old\"}"}`,
				fmt.Sprintf(`data: {"type":"response.function_call_arguments.done","output_index":0,"arguments":%s}`, encoded),
				`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc","call_id":"call","name":"echo","arguments":""}}`,
				`data: {"type":"response.completed","response":{"status":"completed","output":[]}}`,
			}, "\n\n") + "\n\n"
			output := &AssistantMessage{StopReason: StopPending}
			if err := ProcessResponsesStream(context.Background(), strings.NewReader(body), output, NewAssistantMessageEventStream(), testResponsesModel(), nil, nil); err != nil {
				t.Fatal(err)
			}
			if len(output.Content) != 1 {
				t.Fatalf("content = %+v", output.Content)
			}
			call := output.Content[0].(ToolCall)
			if final == `{"command":` {
				if string(call.Arguments) != `{}` || !strings.Contains(call.ArgumentsError, "Received arguments:\n"+final) {
					t.Fatalf("malformed replacement lost: %+v", call)
				}
			} else if string(call.Arguments) != final || call.ArgumentsError != "" {
				t.Fatalf("final arguments = %+v, want %s", call, final)
			}
		})
	}
}

// pi v1.4.0 api/openai-responses-shared.ts backfillReasoningSignatures: Azure
// may supply reasoning encrypted_content only in the terminal response, so the
// merged signature must reach the persisted message (#6409).
func TestResponsesBackfillsEncryptedReasoningContent(t *testing.T) {
	run := func(events []string) *AssistantMessage {
		chunks := make([]string, 0, len(events))
		for _, event := range events {
			chunks = append(chunks, "data: "+event+"\n\n")
		}
		server := sseServer(t, chunks, nil)
		defer server.Close()
		model := testResponsesModel()
		model.BaseURL = server.URL
		stream := StreamOpenAIResponses(model, NormalizeContext(Context{
			Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
		}), &OpenAIResponsesOptions{APIKey: "k"})
		msg, err := stream.Result(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return msg
	}
	itemDone := `{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[{"text":"plan"}]}}`
	added := `{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1"}}`

	t.Run("signature-stringifies-the-decoded-item", func(t *testing.T) {
		msg := run([]string{
			added,
			`{"type":"response.output_item.done","output_index":0,"item":{ "type": "reasoning", "id": "rs_1", "summary": [], "encrypted_content": "enc", "custom": { "z": 1, "a": 2 } }}`,
			`{"type":"response.completed","response":{"status":"completed","output":[]}}`,
		})
		block, ok := msg.Content[0].(ThinkingContent)
		if !ok || block.ThinkingSignature == nil {
			t.Fatalf("content = %+v", msg.Content)
		}
		want := `{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"enc","custom":{"z":1,"a":2}}`
		if *block.ThinkingSignature != want {
			t.Fatalf("signature = %s, want %s", *block.ThinkingSignature, want)
		}
	})

	t.Run("backfilled", func(t *testing.T) {
		msg := run([]string{
			`{"type":"response.created","response":{"id":"resp_1"}}`,
			added,
			itemDone,
			`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"type":"reasoning","id":"rs_1","encrypted_content":"enc-blob"}]}}`,
		})
		block, ok := msg.Content[0].(ThinkingContent)
		if !ok || block.ThinkingSignature == nil {
			t.Fatalf("content = %+v", msg.Content)
		}
		want := `{"type":"reasoning","id":"rs_1","summary":[{"text":"plan"}],"encrypted_content":"enc-blob"}`
		if *block.ThinkingSignature != want {
			t.Fatalf("signature = %s, want %s", *block.ThinkingSignature, want)
		}
	})

	t.Run("empty-terminal-content-is-ignored", func(t *testing.T) {
		msg := run([]string{
			added,
			itemDone,
			`{"type":"response.completed","response":{"status":"completed","output":[{"type":"reasoning","id":"rs_1","encrypted_content":""}]}}`,
		})
		block, ok := msg.Content[0].(ThinkingContent)
		if !ok || block.ThinkingSignature == nil {
			t.Fatalf("content = %+v", msg.Content)
		}
		want := `{"type":"reasoning","id":"rs_1","summary":[{"text":"plan"}]}`
		if *block.ThinkingSignature != want {
			t.Fatalf("signature = %s, want %s", *block.ThinkingSignature, want)
		}
	})

	t.Run("already-present", func(t *testing.T) {
		msg := run([]string{
			`{"type":"response.created","response":{"id":"resp_1"}}`,
			added,
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[{"text":"plan"}],"encrypted_content":"already"}}`,
			`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"type":"reasoning","id":"rs_1","encrypted_content":"other"}]}}`,
		})
		block, ok := msg.Content[0].(ThinkingContent)
		if !ok || block.ThinkingSignature == nil {
			t.Fatalf("content = %+v", msg.Content)
		}
		if strings.Contains(*block.ThinkingSignature, "other") || !strings.Contains(*block.ThinkingSignature, `"already"`) {
			t.Fatalf("signature = %s", *block.ThinkingSignature)
		}
	})

	t.Run("empty-stored-field-is-backfilled", func(t *testing.T) {
		msg := run([]string{
			`{"type":"response.created","response":{"id":"resp_1"}}`,
			added,
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":""}}`,
			`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"type":"reasoning","id":"rs_1","encrypted_content":"filled"}]}}`,
		})
		block, ok := msg.Content[0].(ThinkingContent)
		if !ok || block.ThinkingSignature == nil {
			t.Fatalf("content = %+v", msg.Content)
		}
		want := `{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"filled"}`
		if *block.ThinkingSignature != want {
			t.Fatalf("signature = %s, want %s", *block.ThinkingSignature, want)
		}
	})
}

// pi v1.4.0 (4a080252), api/openai-responses-shared.ts uses
// item.input ?? getCustomToolCallInput(block), not a truthiness fallback.
// Expectations were replayed through the actual upstream stream: absent/null
// input retains accumulated input, but an explicit empty string must not rewind it.
func TestResponsesCustomToolFinalInputDoesNotRewind(t *testing.T) {
	for _, c := range []struct {
		name, finalInput string
		want             StopReason
	}{
		{"missing", "", StopToolUse},
		{"null", `,"input":null`, StopToolUse},
		{"empty", `,"input":""`, StopError},
		{"complete", `,"input":"echo harmless"`, StopToolUse},
	} {
		t.Run(c.name, func(t *testing.T) {
			server := sseServer(t, []string{
				"data: " + `{"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"echo","input":""}}` + "\n\n",
				"data: " + `{"type":"response.custom_tool_call_input.delta","output_index":0,"delta":"echo harmless"}` + "\n\n",
				fmt.Sprintf("data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"custom_tool_call\",\"id\":\"ctc_1\",\"call_id\":\"call_1\",\"name\":\"echo\"%s}}\n\n", c.finalInput),
				"data: " + `{"type":"response.completed","response":{"status":"completed","output":[]}}` + "\n\n",
			}, nil)
			defer server.Close()
			model := testResponsesModel()
			model.BaseURL = server.URL
			result, err := StreamOpenAIResponses(model, TranscriptContext{Messages: []Message{
				&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
			}}, &OpenAIResponsesOptions{StreamOptions: StreamOptions{APIKey: "test-key"}}).Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.StopReason != c.want {
				t.Fatalf("stop = %s, want %s; error = %v", result.StopReason, c.want, result.ErrorMessage)
			}
			if c.want == StopError {
				if result.ErrorMessage == nil || !strings.Contains(*result.ErrorMessage, "changed non-monotonically") {
					t.Fatalf("error = %v", result.ErrorMessage)
				}
			} else if call, ok := result.Content[0].(ToolCall); !ok || string(call.Arguments) != `{"input":"echo harmless"}` {
				t.Fatalf("content = %+v", result.Content)
			}
		})
	}
}

// The request effort comes from the model's thinkingLevelMap when the selected
// level is mapped, and an unmapped level sends its own name
// (openai responses stream.ts). GitHub Copilot omits the reasoning block
// entirely when thinking is off.
func TestOpenAIResponsesReasoningEffortMapping(t *testing.T) {
	mapped := "xhigh"
	model := &Model{
		ID: "gpt-5", Name: "GPT-5", API: APIOpenAIResponses, Provider: "openai",
		BaseURL: "https://api.openai.com/v1", Input: []string{"text"}, Reasoning: true,
		ThinkingLevelMap: ThinkingLevelMap{ThinkHigh: &mapped},
	}
	compat := GetOpenAIResponsesCompat(model)
	context := TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}
	params, err := BuildOpenAIResponsesParams(model, context, &OpenAIResponsesOptions{ReasoningEffort: ThinkHigh}, &compat, nil)
	if err != nil {
		t.Fatal(err)
	}
	if params.Reasoning == nil || params.Reasoning.Effort != "xhigh" {
		t.Fatalf("reasoning = %+v, want the mapped effort xhigh", params.Reasoning)
	}

	copilot := *model
	copilot.Provider = "github-copilot"
	offCompat := GetOpenAIResponsesCompat(&copilot)
	params, err = BuildOpenAIResponsesParams(&copilot, context, nil, &offCompat, nil)
	if err != nil {
		t.Fatal(err)
	}
	if params.Reasoning != nil {
		t.Fatalf("copilot reasoning at off = %+v, want omitted", params.Reasoning)
	}
}

// pi v1.4.0 (4a080252) api/openai-responses-shared.ts convertResponsesTools
// uses a flat grammar format, unlike Chat Completions' nested grammar object.
// Regenerate: node --experimental-strip-types testdata/responses-grammar/generate.mjs ../pi > testdata/responses-grammar/golden.json
func TestResponsesGrammarToolsMatchUpstream(t *testing.T) {
	data, err := os.ReadFile("testdata/responses-grammar/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string
		Tools    []Tool
		Options  ConvertResponsesToolsOptions
		Expected string
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			tools, err := ConvertResponsesTools(fixture.Tools, &fixture.Options)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := MarshalJSON(tools)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != fixture.Expected {
				t.Fatalf("tools = %s\nwant    %s", encoded, fixture.Expected)
			}
		})
	}
}

// Reference: pi v1.4.0 (4a080252), api/openai-responses-shared.ts
// convertResponsesMessages. Compare serialized bytes, including optional ID
// positions, rather than decoded maps that discard JavaScript key order.
// Regenerate: node --experimental-strip-types testdata/responses-input/generate.mjs ../pi > testdata/responses-input/golden.json
func TestResponsesInputMatchesUpstream(t *testing.T) {
	data, err := os.ReadFile("testdata/responses-input/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string
		Model    Model
		Messages []json.RawMessage
		Input    string
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			var messages []Message
			for _, raw := range fixture.Messages {
				message, err := UnmarshalMessage(raw)
				if err != nil {
					t.Fatal(err)
				}
				messages = append(messages, message)
			}
			input, err := ConvertResponsesMessages(&fixture.Model, TranscriptContext{Messages: messages}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if string(input) != fixture.Input {
				t.Fatalf("input = %s\nwant    %s", input, fixture.Input)
			}
		})
	}
}

// Upstream's exemption is credential- and endpoint-specific, not a model-wide
// compat flag. API keys and OpenAI-compatible proxies keep their cache controls.
func TestResponsesChatGPTFieldOmissionIsCredentialSpecific(t *testing.T) {
	for _, test := range []struct {
		name, provider, baseURL, apiKey string
		explicit, omit                  bool
	}{
		{"oauth explicit cache", "openai", OpenAIChatGPTResource, "oauth-token", true, true},
		{"oauth retention", "openai", OpenAIChatGPTResource, "oauth-token", false, true},
		{"api key explicit cache", "openai", OpenAIChatGPTResource, "sk-test", true, false},
		{"api key retention", "openai", OpenAIChatGPTResource, "sk-test", false, false},
		{"proxy token", "openai", "https://proxy.example/v1", "proxy-token", true, false},
		{"other provider", "custom", OpenAIChatGPTResource, "custom-token", true, false},
		{"no credential", "openai", OpenAIChatGPTResource, "", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := testResponsesModel()
			model.Provider, model.BaseURL = test.provider, test.baseURL
			model.Compat = &ModelCompat{OpenAIResponses: &OpenAIResponsesCompat{
				SupportsExplicitPromptCacheMode: new(test.explicit),
			}}
			maxTokens := 4096
			temperature := 0.2
			params, err := BuildOpenAIResponsesParams(model, TranscriptContext{}, &OpenAIResponsesOptions{
				APIKey: test.apiKey, CacheRetention: CacheRetentionLong, SessionID: "session",
				MaxTokens: &maxTokens, Temperature: &temperature,
			}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if test.omit {
				if params.PromptCacheOptions != nil || params.PromptCacheRetention != nil {
					t.Fatal("ChatGPT request retained unsupported cache controls")
				}
			} else if test.explicit {
				if params.PromptCacheOptions == nil || params.PromptCacheOptions.TTL != "30m" {
					t.Fatal("non-ChatGPT request lost explicit cache controls")
				}
			} else if params.PromptCacheRetention == nil || *params.PromptCacheRetention != "24h" {
				t.Fatal("non-ChatGPT request lost cache retention")
			}
			if test.omit {
				if params.MaxOutputTokens != nil || params.Temperature != nil {
					t.Fatal("ChatGPT request retained unsupported sampling controls")
				}
			} else if params.MaxOutputTokens == nil || *params.MaxOutputTokens != 4096 ||
				params.Temperature == nil || *params.Temperature != 0.2 {
				t.Fatal("non-ChatGPT request lost sampling controls")
			}
			if params.PromptCacheKey == nil || *params.PromptCacheKey != "session" {
				t.Fatal("session prompt cache key was not preserved")
			}
		})
	}
}

// Upstream api/openai-responses.ts buildParams omits unsupported fields for
// Sign in with ChatGPT. Compaction uses cacheRetention=none; emitting explicit
// prompt_cache_options in that case makes subscription requests fail with 400.
func TestResponsesChatGPTCompactionOmitsUnsupportedFields(t *testing.T) {
	model := testResponsesModel()
	model.Compat = &ModelCompat{OpenAIResponses: &OpenAIResponsesCompat{
		SupportsExplicitPromptCacheMode: new(true),
	}}
	maxTokens := 4096
	temperature := 0.2
	params, err := BuildOpenAIResponsesParams(model, TranscriptContext{}, &OpenAIResponsesOptions{
		APIKey: "oauth-access-token", CacheRetention: CacheRetentionNone,
		MaxTokens: &maxTokens, Temperature: &temperature,
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalJSON(params)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"prompt_cache_options", "prompt_cache_retention", "max_output_tokens", "temperature"} {
		if value, ok := wire[field]; ok {
			t.Errorf("ChatGPT request contains unsupported %s: %s", field, value)
		}
	}
}

// pi v1.4.0 (4a080252), api/openai-responses.ts getServiceTierCostMultiplier
// and openai-responses-shared.ts finalizeResponse price the returned tier even
// without a request override. Each token class contributes one million tokens
// at rates 1/2/3/4, so the unscaled cost is exactly 10.
func TestResponsesPricesTheReturnedServiceTier(t *testing.T) {
	for _, c := range []struct {
		name, request, response string
		want                    float64
		modelID                 string
	}{
		{"response priority", "", `,"service_tier":"priority"`, 20, ""},
		{"response flex", "", `,"service_tier":"flex"`, 5, ""},
		{"response overrides request", "flex", `,"service_tier":"priority"`, 20, ""},
		{"response default overrides request", "priority", `,"service_tier":"default"`, 10, ""},
		{"request fallback", "priority", "", 20, ""},
		{"default", "", "", 10, ""},
		{"response fast", "", `,"service_tier":"fast"`, 20, ""},
		{"response fast gpt-5.5", "", `,"service_tier":"fast"`, 25, "gpt-5.5"},
		{"request fast fallback", "fast", "", 20, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":3000000,\"output_tokens\":1000000,\"total_tokens\":4000000,\"input_tokens_details\":{\"cached_tokens\":1000000,\"cache_write_tokens\":1000000}}%s}}\n\n", c.response)
			}))
			defer server.Close()
			model := testResponsesModel()
			if c.modelID != "" {
				model.ID = c.modelID
			}
			model.BaseURL = server.URL
			model.Cost = ModelCost{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4}
			result, err := StreamOpenAIResponses(model, TranscriptContext{Messages: []Message{
				&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
			}}, &OpenAIResponsesOptions{StreamOptions: StreamOptions{APIKey: "test-key"}, ServiceTier: c.request}).Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.StopReason != StopStop {
				t.Fatalf("stop = %s, error = %v", result.StopReason, result.ErrorMessage)
			}
			if result.Usage.Cost.Total != c.want {
				t.Fatalf("cost = %+v, want total %v", result.Usage.Cost, c.want)
			}
		})
	}
}

// pi v1.4.0 api/openai-responses.ts plus openai-responses-shared.ts
// (processResponsesStream), executed by the pinned module. Regenerate:
//
//	node --experimental-strip-types ai/testdata/openai-responses-stream/generate.mjs ./pi \
//	  > ai/testdata/openai-responses-stream/golden.json
//
// Only the event's own scalars and the terminal message are compared; see
// anthropic_stream_events_parity_test.go for why `partial` is excluded.
func TestOpenAIResponsesStreamEventsMatchUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/openai-responses-stream/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name   string            `json:"name"`
		SSE    string            `json:"sse"`
		Events []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty openai-responses stream corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(fixture.SSE))
			}))
			defer server.Close()

			model := testResponsesModel()
			model.ID = "gpt-5"
			model.Name = "GPT-5"
			model.BaseURL = server.URL + "/v1"
			model.Cost = ModelCost{Input: 1.25, Output: 10, CacheRead: 0.125}

			stream := StreamOpenAIResponses(model, NormalizeContext(Context{
				Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
			}), &OpenAIResponsesOptions{APIKey: "test-key"})

			checkProjectedEvents(t, stream, fixture.Events)
		})
	}
}
