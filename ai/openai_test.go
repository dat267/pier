package ai

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pi v1.5.2 (b4afdddf) providers/compat-schema.ts added three compat flags.
// Each has a visible wire effect, so a config that sets one must not be ignored.

func v152CompletionsModel(baseURL string, compat *OpenAICompletionsCompat) *Model {
	model := &Model{ID: "test-model", Name: "Test", Provider: "custom", API: APIOpenAICompletions, BaseURL: baseURL}
	if compat != nil {
		model.Compat = &ModelCompat{OpenAICompletions: compat}
	}
	return model
}

func v152Transcript() TranscriptContext {
	return NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	})
}

func buildV152Completions(t *testing.T, model *Model, sessionID string) *OpenAICompletionsParams {
	t.Helper()
	resolved := GetOpenAICompletionsCompat(model)
	params, err := BuildOpenAICompletionsParams(model, v152Transcript(), &OpenAICompletionsOptions{
		SessionID: sessionID, CacheRetention: CacheRetentionShort,
	}, &resolved, CacheRetentionShort, nil)
	if err != nil {
		t.Fatal(err)
	}
	return params
}

// promptCacheKeyMode: "disabled" never sends the key, "enabled" sends it
// whenever caching is active, and "auto" (the default) only to OpenAI or with
// long retention support.
func TestOpenAICompletionsPromptCacheKeyMode(t *testing.T) {
	openAI := "https://api.openai.com/v1"
	for _, test := range []struct {
		name    string
		baseURL string
		mode    string
		want    bool
	}{
		{"auto-openai", openAI, "", true},
		{"auto-custom", "https://example.test/v1", "", false},
		{"disabled-openai", openAI, "disabled", false},
		{"disabled-custom", "https://example.test/v1", "disabled", false},
		{"enabled-custom", "https://example.test/v1", "enabled", true},
		{"enabled-openai", openAI, "enabled", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var compat *OpenAICompletionsCompat
			if test.mode != "" {
				compat = &OpenAICompletionsCompat{PromptCacheKeyMode: new(test.mode)}
			}
			params := buildV152Completions(t, v152CompletionsModel(test.baseURL, compat), "session-1")
			if got := params.PromptCacheKey != nil; got != test.want {
				t.Fatalf("prompt_cache_key present = %v, want %v", got, test.want)
			}
		})
	}
	// A session id is required; without one nothing is sent whatever the mode.
	params := buildV152Completions(t, v152CompletionsModel(openAI, nil), "")
	if params.PromptCacheKey != nil {
		t.Fatal("prompt_cache_key sent without a session id")
	}
}

// requiresNonNullAssistantContent: a replayed assistant message with no text
// sends "" instead of null.
func TestOpenAICompletionsRequiresNonNullAssistantContent(t *testing.T) {
	transcript := NormalizeContext(Context{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
		&AssistantMessage{Content: []Content{ToolCall{ID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"value":"a"}`)}}, Timestamp: 2},
		&ToolResultMessage{ToolCallID: "call-1", ToolName: "lookup", Content: UserContentList{TextContent{Text: "ok"}}, Timestamp: 3},
	}})
	for _, test := range []struct {
		name string
		flag *bool
		want string
	}{
		{"default", nil, "null"},
		{"enabled", new(true), `""`},
		{"explicit-false", new(false), "null"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := v152CompletionsModel("https://example.test/v1", &OpenAICompletionsCompat{RequiresNonNullAssistantContent: test.flag})
			resolved := GetOpenAICompletionsCompat(model)
			params, err := BuildOpenAICompletionsParams(model, transcript, &OpenAICompletionsOptions{
				CacheRetention: CacheRetentionNone,
			}, &resolved, CacheRetentionNone, nil)
			if err != nil {
				t.Fatal(err)
			}
			content := ""
			for _, message := range params.Messages {
				if message.Role == "assistant" {
					content = string(message.Content)
				}
			}
			if content != test.want {
				t.Fatalf("assistant content = %s, want %s", content, test.want)
			}
		})
	}
}

// requiresToolsWithToolHistory: with tool history but no tools in this request,
// send "tools": [] instead of omitting the field.
func TestOpenAICompletionsRequiresToolsWithToolHistory(t *testing.T) {
	transcript := NormalizeContext(Context{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
		&AssistantMessage{Content: []Content{ToolCall{ID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"value":"a"}`)}}, Timestamp: 2},
		&ToolResultMessage{ToolCallID: "call-1", ToolName: "lookup", Content: UserContentList{TextContent{Text: "ok"}}, Timestamp: 3},
	}})
	for _, test := range []struct {
		name     string
		flag     *bool
		wantSent bool
	}{
		{"default", nil, false},
		{"enabled", new(true), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := v152CompletionsModel("https://example.test/v1", &OpenAICompletionsCompat{RequiresToolsWithToolHistory: test.flag})
			resolved := GetOpenAICompletionsCompat(model)
			params, err := BuildOpenAICompletionsParams(model, transcript, &OpenAICompletionsOptions{
				CacheRetention: CacheRetentionNone,
			}, &resolved, CacheRetentionNone, nil)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := MarshalJSON(params)
			if err != nil {
				t.Fatal(err)
			}
			// An empty array is present; an absent field is not sent at all.
			if sent := strings.Contains(string(encoded), `"tools":[]`); sent != test.wantSent {
				t.Fatalf("tools = %s, want sent=%v", encoded, test.wantSent)
			}
		})
	}
	// Without tool history the field stays absent even with the flag on.
	plain := NormalizeContext(Context{Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}}})
	model := v152CompletionsModel("https://example.test/v1", &OpenAICompletionsCompat{RequiresToolsWithToolHistory: new(true)})
	resolved := GetOpenAICompletionsCompat(model)
	params, err := BuildOpenAICompletionsParams(model, plain, &OpenAICompletionsOptions{
		CacheRetention: CacheRetentionNone,
	}, &resolved, CacheRetentionNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalJSON(params)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"tools"`) {
		t.Fatalf("tools sent without tool history: %s", encoded)
	}
}

// On the Responses API the same mode only has to suppress the key.
func TestOpenAIResponsesPromptCacheKeyModeDisabled(t *testing.T) {
	for _, test := range []struct {
		name string
		mode *string
		want bool
	}{
		{"auto", nil, true},
		{"disabled", new("disabled"), false},
		{"enabled", new("enabled"), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := &Model{ID: "test-model", Name: "Test", Provider: "openai", API: APIOpenAIResponses, BaseURL: "https://api.openai.com/v1"}
			model.Compat = &ModelCompat{OpenAIResponses: &OpenAIResponsesCompat{PromptCacheKeyMode: test.mode}}
			params, err := BuildOpenAIResponsesParams(model, v152Transcript(), &OpenAIResponsesOptions{
				SessionID: "session-1", CacheRetention: CacheRetentionShort,
			}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := params.PromptCacheKey != nil; got != test.want {
				t.Fatalf("prompt_cache_key present = %v, want %v", got, test.want)
			}
		})
	}
}

// pi v1.4.0 api/openai-completions.ts stream transitions, executed by the
// pinned module. Regenerate:
//
//	node --experimental-strip-types ai/testdata/openai-stream/generate.mjs ./pi \
//	  > ai/testdata/openai-stream/golden.json
//
// Only the event's own scalars and the terminal message are compared; see
// anthropic_stream_events_parity_test.go for why `partial` is excluded.
func TestOpenAIStreamEventsMatchUpstream(t *testing.T) {
	runStreamEventsCorpus(t, "testdata/openai-stream/golden.json", "openai stream", func(baseURL string) *AssistantMessageEventStream {
		model := testOpenAIModel()
		model.ID = "gpt-5"
		model.Name = "GPT-5"
		model.BaseURL = baseURL + "/v1"
		model.Cost = ModelCost{Input: 1.25, Output: 10, CacheRead: 0.125}

		return StreamOpenAICompletions(model, NormalizeContext(Context{
			Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
		}), &OpenAICompletionsOptions{APIKey: "test-key"})
	})
}

// Reference: pi v1.5.0 (f9a26365), api/openai-completions.ts
// ensureToolCallBlock/getContentIndex retain block identity when an indexed
// delta fills in the id or name after another call has already started.
func TestOpenAICompletionsKeepsInterleavedCallsWithLateIdentity(t *testing.T) {
	for _, first := range []struct {
		name string
		call string
	}{
		{name: "late id", call: `{"index":0,"function":{"name":"read","arguments":"{\"path\":"}}`},
		{name: "late name", call: `{"index":0,"id":"call_a","function":{"arguments":"{\"path\":"}}`},
		{name: "known identity", call: `{"index":0,"id":"call_a","function":{"name":"read","arguments":"{\"path\":"}}`},
	} {
		t.Run(first.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[" + first.call + `,{"index":1,"id":"call_b","function":{"name":"read","arguments":"{\"path\":\"b\"}"}}]}}]}` + "\n\n"))
				_, _ = writer.Write([]byte("data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"read","arguments":"\"a\"}"}}]}}]}` + "\n\n"))
				_, _ = writer.Write([]byte("data: " + `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"))
			}))
			defer server.Close()
			model := testOpenAIModel()
			model.BaseURL = server.URL
			stream := StreamOpenAICompletions(model, NormalizeContext(Context{}), &OpenAICompletionsOptions{APIKey: "test-key"})
			message, err := stream.Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if message.StopReason != StopToolUse || len(message.Content) != 2 {
				t.Fatalf("message = %+v, want two finished calls", message)
			}
			for i, want := range []struct{ id, args string }{
				{id: "call_a", args: `{"path":"a"}`},
				{id: "call_b", args: `{"path":"b"}`},
			} {
				call, ok := message.Content[i].(ToolCall)
				if !ok || call.ID != want.id || call.Name != "read" || string(call.Arguments) != want.args || call.ArgumentsError != "" {
					t.Errorf("content[%d] = %+v, want %s read(%s)", i, message.Content[i], want.id, want.args)
				}
			}
		})
	}
}

// Reference: pi v1.5.0 (f9a26365), api/openai-completions.ts
// parseChunkUsage subtracts cache counts and sums totals as JavaScript Numbers.
// D367 saturates results beyond the port's int64 usage representation.
func TestParseChunkUsageDoesNotWrapHugeCacheOrTotalCounts(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
		want Usage
	}{
		{
			name: "large total",
			raw:  `{"prompt_tokens":6000000000000000000,"completion_tokens":6000000000000000000}`,
			want: Usage{Input: 6000000000000000000, Output: 6000000000000000000, TotalTokens: math.MaxInt64},
		},
		{
			name: "large cache subtraction",
			raw:  `{"prompt_tokens":100,"prompt_tokens_details":{"cached_tokens":9223372036854775807,"cache_write_tokens":9223372036854775807}}`,
			want: Usage{CacheRead: math.MaxInt64, CacheWrite: math.MaxInt64, TotalTokens: math.MaxInt64},
		},
		{
			name: "ordinary cached usage",
			raw:  `{"prompt_tokens":100,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":20,"cache_write_tokens":10}}`,
			want: Usage{Input: 70, Output: 4, CacheRead: 20, CacheWrite: 10, TotalTokens: 104},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := ParseChunkUsage([]byte(test.raw), testOpenAIModel())
			if got.Input != test.want.Input || got.Output != test.want.Output || got.CacheRead != test.want.CacheRead || got.CacheWrite != test.want.CacheWrite || got.TotalTokens != test.want.TotalTokens {
				t.Fatalf("usage = %+v, want token fields %+v", got, test.want)
			}
		})
	}
}

// jsJSONTruthy follows JavaScript: only null, false, 0 and "" are falsy. A
// number that overflows to Infinity and one that underflows to zero are the two
// edges Go's decoder does not handle the way JSON.parse does, so both are pinned
// here. The corpus has neither, and the branch got the success case backwards:
// 1e-400 decodes to 0 with no error and was reported truthy.
func TestJSJSONTruthyMatchesJavaScriptOnOverflowAndUnderflow(t *testing.T) {
	// JSON.parse("1e400") is Infinity, which is truthy.
	// JSON.parse("1e-400") is 0, which is falsy.
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"1e-400", false},
		{"-1e-400", false},
		{"1e400", true},
		{"-1e400", true},
		{"1e-323", true},
	} {
		if got := jsJSONTruthy(json.RawMessage(tc.raw)); got != tc.want {
			t.Fatalf("jsJSONTruthy(%s) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}
