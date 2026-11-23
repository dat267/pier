package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// openai-responses tests keyed to upstream (openai-responses.ts +
// -shared.ts): compat resolution, params shape, message conversion, and the
// SSE event loop.

// Upstream v1.4.0 #10930/#10853: with supportsMidConvoEffort, changing the
// thinking level is sent as configuration_update items at a fixed
// reasoning.effort of "medium", so earlier input (and the prompt cache) does
// not change. The reply records the level so the next request replays it.
func TestConfigurationUpdateEffortKeepsThePromptCache(t *testing.T) {
	model := testResponsesModel()
	model.Compat = &ModelCompat{OpenAIResponses: &OpenAIResponsesCompat{
		SupportsMidConvoEffort: new(true),
	}}
	model.ThinkingLevelMap = map[ThinkingLevel]*string{ThinkHigh: new("high")}
	ctx := NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	})
	params, err := BuildOpenAIResponsesParams(model, ctx, &OpenAIResponsesOptions{
		APIKey:          "sk-test",
		ReasoningEffort: ThinkHigh,
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if params.Reasoning == nil || params.Reasoning.Effort != "medium" {
		t.Fatalf("reasoning = %+v; want the fixed medium effort", params.Reasoning)
	}
	var items []map[string]any
	if err := json.Unmarshal(params.Input, &items); err != nil {
		t.Fatal(err)
	}
	last := items[len(items)-1]
	if last["type"] != "configuration_update" {
		t.Fatalf("last item = %+v", last)
	}
	reasoning, _ := last["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" {
		t.Fatalf("configuration_update = %+v", last)
	}

	// Sign in with ChatGPT rejects configuration_update items.
	oauth := testResponsesModel()
	oauth.BaseURL = OpenAIChatGPTResource
	oauth.Compat = model.Compat
	oauth.ThinkingLevelMap = model.ThinkingLevelMap
	effort := getConfigurationUpdateEffort(oauth, &OpenAIResponsesOptions{
		APIKey:          "oauth-token",
		ReasoningEffort: ThinkHigh,
	}, GetOpenAIResponsesCompat(oauth))
	if effort != "" {
		t.Fatalf("ChatGPT sign-in effort = %q; want none", effort)
	}

	// A later request replays the level recorded on an earlier reply, before it.
	previous := &AssistantMessage{
		Content: ContentList{TextContent{Text: "answer"}},
		API:     model.API, Provider: model.Provider, Model: model.ID,
		StopReason: StopStop, Timestamp: 2,
	}
	level := "low"
	previous.ProviderThinkingLevel = &level
	input, err := ConvertResponsesMessages(model, NormalizeContext(Context{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
		previous,
	}}), &ConvertResponsesMessagesOptions{ConfigurationUpdateEffort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	var replay []map[string]any
	if err := json.Unmarshal(input, &replay); err != nil {
		t.Fatal(err)
	}
	var levels []string
	for _, item := range replay {
		if item["type"] != "configuration_update" {
			continue
		}
		reasoning, _ := item["reasoning"].(map[string]any)
		effort, _ := reasoning["effort"].(string)
		levels = append(levels, effort)
	}
	if len(levels) != 2 || levels[0] != "low" || levels[1] != "high" {
		t.Fatalf("configuration_update levels = %v", levels)
	}
}

func TestResponsesCompatAndCacheFields(t *testing.T) {
	compat := GetOpenAIResponsesCompat(testResponsesModel())
	if !compat.SupportsDeveloperRole || !compat.SupportsLongCacheRetention || !compat.SupportsMaxOutputTokens {
		t.Fatalf("compat = %+v", compat)
	}
	// Long retention without explicit cache mode → 24h field.
	if retention := GetPromptCacheRetention(compat, CacheRetentionLong); retention != "24h" {
		t.Fatalf("retention = %q", retention)
	}
	if options := GetPromptCacheOptions(compat, CacheRetentionLong); options != nil {
		t.Fatalf("options = %+v; want nil without explicit mode", options)
	}

	// Explicit prompt-cache mode: none → {mode: explicit}; long → ttl 30m;
	// and the 24h retention field is suppressed.
	model := testResponsesModel()
	model.Compat = &ModelCompat{OpenAIResponses: &OpenAIResponsesCompat{
		SupportsExplicitPromptCacheMode: new(true),
	}}
	compat = GetOpenAIResponsesCompat(model)
	if retention := GetPromptCacheRetention(compat, CacheRetentionLong); retention != "" {
		t.Fatalf("retention = %q; explicit mode suppresses it", retention)
	}
	if options := GetPromptCacheOptions(compat, CacheRetentionNone); options == nil || options.Mode != "explicit" {
		t.Fatalf("none options = %+v", options)
	}
	if options := GetPromptCacheOptions(compat, CacheRetentionLong); options == nil || options.TTL != "30m" {
		t.Fatalf("long options = %+v", options)
	}

	// OpenRouter session-affinity format detection.
	openRouter := testResponsesModel()
	openRouter.Provider = "openrouter"
	if format := detectOpenAIResponsesSessionAffinity(openRouter); format != SessionAffinityOpenRouter {
		t.Fatalf("affinity = %s", format)
	}
}

// Upstream openai-responses.ts omits maxTokens when its JavaScript value is falsy.
func TestBuildResponsesParamsOmitsZeroMaxTokens(t *testing.T) {
	zero := 0
	params, err := BuildOpenAIResponsesParams(testResponsesModel(), NormalizeContext(Context{}),
		&OpenAIResponsesOptions{MaxTokens: &zero}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if params.MaxOutputTokens != nil {
		t.Fatalf("zero maxTokens emitted: %+v", params.MaxOutputTokens)
	}
}

func TestBuildResponsesParams(t *testing.T) {
	model := testResponsesModel()
	tool := Tool{Name: "read_file", Description: "Reads", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}
	ctx := NormalizeContext(Context{
		SystemPrompt: new("sys"),
		Messages:     []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
		Tools:        []Tool{tool},
	})
	maxTokens := 8 // below the API minimum of 16
	params, err := BuildOpenAIResponsesParams(model, ctx, &OpenAIResponsesOptions{
		SessionID: "sess", MaxTokens: &maxTokens,
		CacheRetention:  CacheRetentionLong,
		ReasoningEffort: ThinkHigh,
		ServiceTier:     "flex",
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := MarshalJSON(params)
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(enc, &probe); err != nil {
		t.Fatal(err)
	}
	// max_output_tokens clamps UP to the API minimum (issue #6265).
	if string(probe["max_output_tokens"]) != "16" {
		t.Fatalf("max_output_tokens = %s; want 16 (clamped up)", probe["max_output_tokens"])
	}
	if string(probe["store"]) != "false" {
		t.Fatalf("store = %s", probe["store"])
	}
	if string(probe["prompt_cache_key"]) != `"sess"` {
		t.Fatalf("prompt_cache_key = %s", probe["prompt_cache_key"])
	}
	if string(probe["prompt_cache_retention"]) != `"24h"` {
		t.Fatalf("prompt_cache_retention = %s", probe["prompt_cache_retention"])
	}
	if string(probe["service_tier"]) != `"flex"` {
		t.Fatalf("service_tier = %s", probe["service_tier"])
	}
	// Reasoning effort + summary + encrypted-content include.
	if !strings.Contains(string(probe["reasoning"]), `"effort":"high"`) ||
		!strings.Contains(string(probe["reasoning"]), `"summary":"auto"`) {
		t.Fatalf("reasoning = %s", probe["reasoning"])
	}
	if string(probe["include"]) != `["reasoning.encrypted_content"]` {
		t.Fatalf("include = %s", probe["include"])
	}
	var tools []OpenAIResponsesTool
	if err := json.Unmarshal(probe["tools"], &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Type != "function" || tools[0].Name != "read_file" {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestConvertResponsesMessages(t *testing.T) {
	model := testResponsesModel()
	messages := []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
		&AssistantMessage{
			Content: ContentList{
				ThinkingContent{Thinking: "hmm", ThinkingSignature: new(`{"type":"reasoning","id":"rs_1","summary":[]}`)},
				TextContent{Text: "answer"},
				ToolCall{ID: "call_1|fc_1", Name: "bash", Arguments: json.RawMessage(`{"cmd":"ls"}`)},
			},
			API: APIOpenAIResponses, Provider: "openai", Model: "gpt-5",
			StopReason: StopToolUse, Timestamp: 2,
		},
		&ToolResultMessage{
			ToolCallID: "call_1|fc_1", ToolName: "bash",
			Content: UserContentList{TextContent{Text: "out"}}, Timestamp: 3,
		},
	}
	input, err := ConvertResponsesMessages(model, TranscriptContext{Messages: messages}, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := string(input)
	// Reasoning items replay verbatim as their raw signature JSON.
	if !strings.Contains(text, `{"type":"reasoning","id":"rs_1","summary":[]}`) {
		t.Fatalf("reasoning replay missing: %s", text)
	}
	// Assistant text becomes a message item with an output_text part.
	if !strings.Contains(text, `"type":"message"`) || !strings.Contains(text, `"type":"output_text"`) {
		t.Fatalf("message item missing: %s", text)
	}
	// Tool call keeps call_id|item_id for OpenAI-family providers.
	if !strings.Contains(text, `"type":"function_call"`) || !strings.Contains(text, `"call_id":"call_1"`) ||
		!strings.Contains(text, `"id":"fc_1"`) {
		t.Fatalf("function_call missing: %s", text)
	}
	// Tool result becomes function_call_output keyed by call_id only.
	if !strings.Contains(text, `"type":"function_call_output"`) || !strings.Contains(text, `"output":"out"`) {
		t.Fatalf("function_call_output missing: %s", text)
	}
}

func TestConvertResponsesToolResultWithImages(t *testing.T) {
	model := testResponsesModel()
	output := convertResponsesToolResultOutput(model, UserContentList{
		TextContent{Text: "caption"},
		ImageContent{Data: "aGk=", MimeType: "image/png"},
	})
	list, ok := output.([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("output = %+v", output)
	}
	encoded, _ := MarshalJSON(list)
	if !strings.Contains(string(encoded), `"type":"input_text"`) ||
		!strings.Contains(string(encoded), `"image_url":"data:image/png;base64,aGk="`) {
		t.Fatalf("encoded = %s", encoded)
	}

	// Non-vision models get the placeholder text instead.
	noVision := testResponsesModel()
	noVision.Input = []string{"text"}
	if got := convertResponsesToolResultOutput(noVision, UserContentList{ImageContent{Data: "aGk=", MimeType: "image/png"}}); got != "(see attached image)" {
		t.Fatalf("non-vision output = %v", got)
	}
	if got := convertResponsesToolResultOutput(model, UserContentList{}); got != "(no tool output)" {
		t.Fatalf("empty output = %v", got)
	}
}

func TestServiceTierPricing(t *testing.T) {
	model := testResponsesModel()
	usage := Usage{Cost: UsageCost{Input: 2, Output: 4, CacheRead: 1, CacheWrite: 1, Total: 8}}
	applyServiceTierPricing(&usage, "flex", model)
	if usage.Cost.Input != 1 || usage.Cost.Output != 2 || usage.Cost.Total != 4 {
		t.Fatalf("flex pricing = %+v", usage.Cost)
	}
	usage = Usage{Cost: UsageCost{Input: 1, Output: 1, Total: 2}}
	applyServiceTierPricing(&usage, "priority", model)
	if usage.Cost.Input != 2 || usage.Cost.Total != 4 {
		t.Fatalf("priority pricing = %+v", usage.Cost)
	}
	if getServiceTierCostMultiplier(&Model{ID: "gpt-5.5"}, "priority") != 2.5 {
		t.Fatal("gpt-5.5 priority multiplier")
	}
}

// TestConvertResponsesMessagesDropsMismatchedItemIds ports constrained-sampling.test.ts
// "drops foreign item ids when replaying grammar calls as custom Responses
// items" (bc2d8dc1c, radius#115): foreign tool call ids are normalized to fc_*,
// but a grammar tool replays as custom_tool_call, which requires ctc_* ids, so
// the mismatched id is dropped rather than sent.
func TestConvertResponsesMessagesDropsMismatchedItemIds(t *testing.T) {
	model := testResponsesModel()
	messages := []Message{
		&AssistantMessage{
			Content: ContentList{
				ToolCall{ID: "call_1|ctc_1", Name: "sample_tool", Arguments: json.RawMessage(`{"payload":"abc"}`)},
			},
			API: "pi-messages", Provider: "radius", Model: "gpt-other",
			StopReason: StopToolUse, Timestamp: 1,
		},
		&ToolResultMessage{
			ToolCallID: "call_1|ctc_1", ToolName: "sample_tool",
			Content: UserContentList{TextContent{Text: "done"}}, Timestamp: 2,
		},
	}
	input, err := ConvertResponsesMessages(model, TranscriptContext{Messages: messages},
		&ConvertResponsesMessagesOptions{GrammarToolInputProperties: map[string]string{"sample_tool": "payload"}})
	if err != nil {
		t.Fatal(err)
	}
	// The custom_tool_call item must carry no id at all: the foreign id was
	// normalized to fc_* (openai_responses normalization), and a
	// custom_tool_call requires ctc_* (upstream asserts `call.id` undefined).
	var items []map[string]any
	if err := json.Unmarshal(input, &items); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item["type"] != "custom_tool_call" {
			continue
		}
		if item["call_id"] != "call_1" || item["input"] != "abc" {
			t.Fatalf("custom_tool_call = %+v", item)
		}
		if _, hasID := item["id"]; hasID {
			t.Fatalf("mismatched id replayed: %+v", item)
		}
		return
	}
	t.Fatal("no custom_tool_call item")
}

// A whitespace-only tool result is empty as far as the provider is concerned.
// Upstream v1.4.0 4dd2b149d (issue #8720): some providers reject whitespace-only
// tool content with a 400, and the failure then repeats on every request in the
// session, so a bare "\r\n" from bash uses the empty-output placeholder.
func TestResponsesToolResultWhitespaceOnlyUsesPlaceholder(t *testing.T) {
	model := testResponsesModel()
	for _, text := range []string{"", " ", "\r\n", "\n\n\t ", "\u00A0", "\uFEFF"} {
		got := convertResponsesToolResultOutput(model, UserContentList{TextContent{Text: text}})
		if got != "(no tool output)" {
			t.Fatalf("output for %q = %v; want the empty-output placeholder", text, got)
		}
	}
	// Text with any visible content still travels verbatim, whitespace included.
	got := convertResponsesToolResultOutput(model, UserContentList{TextContent{Text: " \r\nout\r\n "}})
	if got != " \r\nout\r\n " {
		t.Fatalf("output = %v; want the text verbatim", got)
	}
}

// ChatGPT subscription limits split in two. Upstream v1.4.0 cedefc532: the
// shared subscription limit keeps pointing at the usage page, while a ChatGPT
// Business per-user workspace limit tells the user to ask their workspace admin.
func TestOpenAIResponsesChatGPTLimitHints(t *testing.T) {
	hint := "You've reached your ChatGPT workspace's usage limit. Ask your workspace admin to increase it at admin.openai.com → Workspace → Usage limits."
	cases := []struct {
		name string
		code string
		want []string
		deny []string
	}{
		{
			name: "shared subscription limit",
			code: "subscription_sharing_usage_limit_exceeded",
			want: []string{"Check your ChatGPT usage: " + OpenAIChatGPTUsageURL},
			deny: []string{hint},
		},
		{
			name: "workspace per-user limit",
			code: "subscription_sharing_workplace_individual_usage_limit_exceeded",
			want: []string{hint},
			deny: []string{OpenAIChatGPTUsageURL},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			body := `{"error":{"type":"invalid_request_error","code":"` + testCase.code + `","message":"usage limit reached"}}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			model := testResponsesModel()
			model.BaseURL = server.URL
			stream := StreamOpenAIResponses(model, NormalizeContext(Context{}),
				&OpenAIResponsesOptions{APIKey: "k"})
			msg, _ := stream.Result(context.Background())
			if msg.ErrorMessage == nil {
				t.Fatalf("message = %+v; want a provider error", msg)
			}
			for _, want := range testCase.want {
				if !strings.Contains(*msg.ErrorMessage, want) {
					t.Fatalf("error = %q; want it to contain %q", *msg.ErrorMessage, want)
				}
			}
			for _, deny := range testCase.deny {
				if strings.Contains(*msg.ErrorMessage, deny) {
					t.Fatalf("error = %q; must not contain %q", *msg.ErrorMessage, deny)
				}
			}
		})
	}
}

// pi v1.5.2 (b4afdddf) openai-responses.ts sends the OpenAI session-affinity
// header as `session-id`; v1.5.0 used `session_id`. A gateway configured with
// sessionAffinityFormat "openai" routed on the hyphenated name, so pier's turns
// of one conversation went to different upstreams and missed the prompt cache.
func TestResponsesSendsHyphenatedSessionAffinityHeader(t *testing.T) {
	headers := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`+"\n\n")
	}))
	defer server.Close()

	format := SessionAffinityOpenAI
	model := testResponsesModel()
	model.BaseURL = server.URL
	model.Compat = &ModelCompat{OpenAIResponses: &OpenAIResponsesCompat{SessionAffinityFormat: &format}}
	stream := StreamOpenAIResponses(model, TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}, &OpenAIResponsesOptions{APIKey: "sk-test", SessionID: "sess-1"})
	if _, err := stream.Result(context.Background()); err != nil {
		t.Fatalf("stream: %v", err)
	}

	got := <-headers
	if value := got.Get("session-id"); value != "sess-1" {
		t.Fatalf("session-id = %q, want sess-1", value)
	}
	if value := got.Get("session_id"); value != "" {
		t.Fatalf("session_id = %q; the OpenAI affinity header is hyphenated", value)
	}
	if value := got.Get("x-client-request-id"); value != "sess-1" {
		t.Fatalf("x-client-request-id = %q, want sess-1", value)
	}
}
