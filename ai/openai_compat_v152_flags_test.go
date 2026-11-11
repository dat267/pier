package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// pi v1.5.2 (b4afdddf) providers/compat-schema.ts added three compat flags.
// Each has a visible wire effect, so a config that sets one must not be ignored.

func compatBoolPtr(value bool) *bool       { return &value }
func compatStringPtr(value string) *string { return &value }

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
		StreamOptions: StreamOptions{SessionID: sessionID, CacheRetention: CacheRetentionShort},
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
				compat = &OpenAICompletionsCompat{PromptCacheKeyMode: compatStringPtr(test.mode)}
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
		{"enabled", compatBoolPtr(true), `""`},
		{"explicit-false", compatBoolPtr(false), "null"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := v152CompletionsModel("https://example.test/v1", &OpenAICompletionsCompat{RequiresNonNullAssistantContent: test.flag})
			resolved := GetOpenAICompletionsCompat(model)
			params, err := BuildOpenAICompletionsParams(model, transcript, &OpenAICompletionsOptions{
				StreamOptions: StreamOptions{CacheRetention: CacheRetentionNone},
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
		{"enabled", compatBoolPtr(true), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := v152CompletionsModel("https://example.test/v1", &OpenAICompletionsCompat{RequiresToolsWithToolHistory: test.flag})
			resolved := GetOpenAICompletionsCompat(model)
			params, err := BuildOpenAICompletionsParams(model, transcript, &OpenAICompletionsOptions{
				StreamOptions: StreamOptions{CacheRetention: CacheRetentionNone},
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
	model := v152CompletionsModel("https://example.test/v1", &OpenAICompletionsCompat{RequiresToolsWithToolHistory: compatBoolPtr(true)})
	resolved := GetOpenAICompletionsCompat(model)
	params, err := BuildOpenAICompletionsParams(model, plain, &OpenAICompletionsOptions{
		StreamOptions: StreamOptions{CacheRetention: CacheRetentionNone},
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
		{"disabled", compatStringPtr("disabled"), false},
		{"enabled", compatStringPtr("enabled"), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := &Model{ID: "test-model", Name: "Test", Provider: "openai", API: APIOpenAIResponses, BaseURL: "https://api.openai.com/v1"}
			model.Compat = &ModelCompat{OpenAIResponses: &OpenAIResponsesCompat{PromptCacheKeyMode: test.mode}}
			params, err := BuildOpenAIResponsesParams(model, v152Transcript(), &OpenAIResponsesOptions{
				StreamOptions: StreamOptions{SessionID: "session-1", CacheRetention: CacheRetentionShort},
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
