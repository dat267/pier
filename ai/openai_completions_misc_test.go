package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), api/openai-completions.ts falls
// back to choice.usage when !chunk.usage, including an explicit null.
func TestOpenAICompletionsUsesChoiceUsageWhenTopLevelIsNull(t *testing.T) {
	for _, test := range []struct {
		name  string
		field string
		input int64
	}{
		{name: "null", field: `"usage":null,`, input: 10},
		{name: "missing", input: 10},
		{name: "top level wins", field: `"usage":{"prompt_tokens":20,"completion_tokens":4},`, input: 20},
		{name: "empty top level wins", field: `"usage":{},`, input: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = writer.Write([]byte("data: {" + test.field + `"choices":[{"finish_reason":"stop","delta":{},"usage":{"prompt_tokens":10,"completion_tokens":4}}]}` + "\n\ndata: [DONE]\n\n"))
			}))
			defer server.Close()
			model := testOpenAIModel()
			model.BaseURL = server.URL
			stream := StreamOpenAICompletions(model, NormalizeContext(Context{}), &OpenAICompletionsOptions{APIKey: "test-key"})
			message, err := stream.Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if message.StopReason != StopStop {
				t.Fatalf("stop reason = %s, error = %v", message.StopReason, message.ErrorMessage)
			}
			if message.Usage.Input != test.input {
				t.Fatalf("input usage = %d, want %d", message.Usage.Input, test.input)
			}
		})
	}
}

// openai-completions.ts baseten branch: `mappedEffort = requestedEffort ?
// map?.[requestedEffort] : map?.off`, then `effort = mappedEffort === undefined
// ? requestedEffort : mappedEffort`, sent only when it is a string. At thinking
// off the requested effort is undefined, so an absent off mapping sends nothing
// rather than the literal "off".
func TestBuildOpenAICompletionsParamsBasetenOffEffort(t *testing.T) {
	build := func(levels ThinkingLevelMap) *OpenAICompletionsParams {
		model := &Model{
			ID: "baseten-model", API: APIOpenAICompletions, Provider: "baseten",
			Reasoning: true, ThinkingLevelMap: levels, ContextWindow: 100000, MaxTokens: 4096,
		}
		compat := GetOpenAICompletionsCompat(model)
		compat.ThinkingFormat = "baseten"
		compat.SupportsReasoningEffort = true
		params, err := BuildOpenAICompletionsParams(model, TranscriptContext{Messages: []Message{
			&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
		}}, &OpenAICompletionsOptions{}, &compat, CacheRetentionNone, map[string]string{})
		if err != nil {
			t.Fatal(err)
		}
		return params
	}

	if params := build(nil); params.ReasoningEffort != nil {
		t.Fatalf("reasoning_effort = %q at off with no off mapping, want none", *params.ReasoningEffort)
	}
	none := "none"
	if params := build(ThinkingLevelMap{ThinkOff: &none}); params.ReasoningEffort == nil || *params.ReasoningEffort != "none" {
		t.Fatalf("reasoning_effort = %v with an off mapping, want none", params.ReasoningEffort)
	}
}

// openai-completions.ts applyAnthropicCacheControl: with cacheControlFormat
// "anthropic" and a retention other than none, the request marks the first
// system/developer message, the last tool, and the last user/assistant/tool
// message, so the provider caches the prefix up to each breakpoint.
func TestBuildOpenAICompletionsParamsAppliesAnthropicCacheControl(t *testing.T) {
	model := testOpenAIModel()
	compat := GetOpenAICompletionsCompat(model)
	compat.CacheControlFormat = "anthropic"
	tools := []Tool{{Name: "read", Description: "read a file", Parameters: json.RawMessage(`{"type":"object"}`)}}
	context := TranscriptContext{
		Messages: []Message{
			&SystemMessage{Content: StringOrBlocks{Text: "instructions"}, ToolsAdded: tools, Timestamp: 0},
			&UserMessage{Content: StringOrBlocks{Text: "first"}, Timestamp: 1},
			&AssistantMessage{Content: ContentList{TextContent{Text: "answer"}}, Timestamp: 2},
			&UserMessage{Content: StringOrBlocks{Text: "second"}, Timestamp: 3},
		},
	}

	params, err := BuildOpenAICompletionsParams(model, context, nil, &compat, CacheRetentionShort, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	// First system message: string content becomes a cached text part.
	if role := params.Messages[0].Role; role != "system" && role != "developer" {
		t.Fatalf("first message role = %q, want the instruction message", role)
	}
	var systemParts []OpenAIContentPart
	if err := json.Unmarshal(params.Messages[0].Content, &systemParts); err != nil {
		t.Fatalf("system content = %s: %v", params.Messages[0].Content, err)
	}
	if len(systemParts) != 1 || systemParts[0].CacheControl == nil || systemParts[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("system parts = %+v, want one cached text part", systemParts)
	}
	// Last tool carries the marker.
	if len(params.Tools) != 1 || params.Tools[0].CacheControl == nil {
		t.Fatalf("tools = %+v, want the last tool marked", params.Tools)
	}
	// Last conversation message carries it.
	last := params.Messages[len(params.Messages)-1]
	var lastParts []OpenAIContentPart
	if err := json.Unmarshal(last.Content, &lastParts); err != nil {
		t.Fatalf("last content = %s: %v", last.Content, err)
	}
	if len(lastParts) != 1 || lastParts[0].CacheControl == nil {
		t.Fatalf("last message parts = %+v, want one cached text part", lastParts)
	}
	// Earlier conversation messages stay unmarked (a plain string content is
	// left alone, which is the marker's absence).
	for _, index := range []int{1, 2} {
		var earlier []OpenAIContentPart
		if err := json.Unmarshal(params.Messages[index].Content, &earlier); err == nil && len(earlier) > 0 && earlier[0].CacheControl != nil {
			t.Fatalf("message %d must not be cached: %s", index, params.Messages[index].Content)
		}
	}

	// Retention none adds no marker at all.
	params, err = BuildOpenAICompletionsParams(model, context, nil, &compat, CacheRetentionNone, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range params.Messages {
		if len(message.Content) > 0 && jsonContainsRaw(message.Content, "cache_control") {
			t.Fatalf("retention none marked %s: %s", message.Role, message.Content)
		}
	}
	if len(params.Tools) == 1 && params.Tools[0].CacheControl != nil {
		t.Fatal("retention none marked the last tool")
	}

	// A compat that is not anthropic adds no marker either.
	other := GetOpenAICompletionsCompat(model)
	other.CacheControlFormat = "openai"
	params, err = BuildOpenAICompletionsParams(model, context, nil, &other, CacheRetentionShort, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range params.Messages {
		if len(message.Content) > 0 && jsonContainsRaw(message.Content, "cache_control") {
			t.Fatalf("openai cache format marked %s: %s", message.Role, message.Content)
		}
	}
}

func jsonContainsRaw(raw json.RawMessage, needle string) bool {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	encoded, err := json.Marshal(value)
	return err == nil && strings.Contains(string(encoded), needle)
}

// openai-completions.ts resolveChatTemplateKwargValue: a kwarg value is a
// literal, a `$var` object, or (the default branch) the thinking level mapped
// through the model's thinkingLevelMap. `map?.[level] === undefined` falls back
// to the requested level name, an explicit null mapping omits the key, and the
// off level has no name to fall back to, so an absent `off` mapping omits it.
func TestBuildChatTemplateValuesMatchesTheReference(t *testing.T) {
	raw := func(text string) json.RawMessage { return json.RawMessage(text) }
	mapped := func(_ ThinkingLevel, value string) *string { return new(value) }
	levels := func(entries map[ThinkingLevel]*string) ThinkingLevelMap {
		if entries == nil {
			return nil
		}
		return ThinkingLevelMap(entries)
	}

	for _, testCase := range []struct {
		name    string
		values  map[string]json.RawMessage
		levels  ThinkingLevelMap
		effort  ThinkingLevel
		budget  *int
		want    map[string]any
		omitted []string
	}{
		{
			name:   "literal values pass through",
			values: map[string]json.RawMessage{"temperature": raw("0.7"), "flag": raw("true"), "text": raw(`"x"`)},
			want:   map[string]any{"temperature": 0.7, "flag": true, "text": "x"},
		},
		{
			name:   "thinking.enabled follows the level",
			values: map[string]json.RawMessage{"enable_thinking": raw(`{"$var":"thinking.enabled"}`)},
			effort: ThinkHigh,
			want:   map[string]any{"enable_thinking": true},
		},
		{
			name:   "thinking.enabled is false when off",
			values: map[string]json.RawMessage{"enable_thinking": raw(`{"$var":"thinking.enabled"}`)},
			effort: "",
			want:   map[string]any{"enable_thinking": false},
		},
		{
			name:   "thinking.budget uses the clamped budget",
			values: map[string]json.RawMessage{"thinking_budget": raw(`{"$var":"thinking.budget"}`)},
			effort: ThinkHigh,
			budget: new(4096),
			want:   map[string]any{"thinking_budget": 4096},
		},
		{
			name:    "thinking.budget without a budget omits the key",
			values:  map[string]json.RawMessage{"thinking_budget": raw(`{"$var":"thinking.budget"}`)},
			effort:  ThinkHigh,
			omitted: []string{"thinking_budget"},
		},
		{
			name:    "omitWhenOff drops the key when off",
			values:  map[string]json.RawMessage{"reasoning": raw(`{"$var":"thinking.level","omitWhenOff":true}`)},
			effort:  "",
			omitted: []string{"reasoning"},
		},
		{
			name:   "mapped level uses the mapping",
			values: map[string]json.RawMessage{"reasoning": raw(`{"$var":"thinking.level"}`)},
			levels: levels(map[ThinkingLevel]*string{ThinkHigh: mapped(ThinkHigh, "HIGH")}),
			effort: ThinkHigh,
			want:   map[string]any{"reasoning": "HIGH"},
		},
		{
			name:   "unmapped level falls back to the level name",
			values: map[string]json.RawMessage{"reasoning": raw(`{"$var":"thinking.level"}`)},
			effort: ThinkMedium,
			want:   map[string]any{"reasoning": "medium"},
		},
		{
			name:    "explicit null mapping omits the key",
			values:  map[string]json.RawMessage{"reasoning": raw(`{"$var":"thinking.level"}`)},
			levels:  levels(map[ThinkingLevel]*string{ThinkHigh: nil}),
			effort:  ThinkHigh,
			omitted: []string{"reasoning"},
		},
		{
			name:   "off with a mapping uses it",
			values: map[string]json.RawMessage{"reasoning": raw(`{"$var":"thinking.level"}`)},
			levels: levels(map[ThinkingLevel]*string{ThinkOff: mapped(ThinkOff, "none")}),
			effort: "",
			want:   map[string]any{"reasoning": "none"},
		},
		{
			// The reference's expression has no level name to fall back to when
			// off and the model has no off mapping, so the key is omitted.
			name:    "off without a mapping omits the key",
			values:  map[string]json.RawMessage{"reasoning": raw(`{"$var":"thinking.level"}`)},
			effort:  "",
			omitted: []string{"reasoning"},
		},
		{
			// resolveChatTemplateKwargValue returns the value unchanged only when
			// it is not an object: an object or array without $var reaches the
			// reasoning-effort fallback like any other variable form, so the
			// reference sends "high", not the literal object.
			name:   "an object without $var resolves to the reasoning effort",
			values: map[string]json.RawMessage{"extra": raw(`{"a":1}`)},
			effort: "high",
			want:   map[string]any{"extra": "high"},
		},
		{
			name:   "an array resolves to the reasoning effort",
			values: map[string]json.RawMessage{"extra": raw(`[1,2]`)},
			effort: "high",
			want:   map[string]any{"extra": "high"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			model := &Model{ID: "m", API: APIOpenAICompletions, ThinkingLevelMap: testCase.levels}
			options := &OpenAICompletionsOptions{ReasoningEffort: string(testCase.effort)}
			got := buildChatTemplateValues(model, options, testCase.values, testCase.budget)
			for key, want := range testCase.want {
				if value, ok := got[key]; !ok || value != want {
					t.Fatalf("%s = %v (present %v), want %v", key, value, ok, want)
				}
			}
			for _, key := range testCase.omitted {
				if value, ok := got[key]; ok {
					t.Fatalf("%s = %v, want the key omitted", key, value)
				}
			}
		})
	}
}

// pi v1.4.0 (4a080252), packages/ai/src/api/openai-completions.ts buildParams
// openrouter branch: with no requested effort, `model.thinkingLevelMap?.off ?? "none"`
// is sent. A missing off mapping therefore yields "none", not the literal "off".
func TestOpenRouterOffReasoningEffortDefaultsToNone(t *testing.T) {
	newParams := func(model *Model, options *OpenAICompletionsOptions, retention CacheRetention) *OpenAICompletionsParams {
		t.Helper()
		compat := DetectOpenAICompletionsCompat(model)
		params, err := BuildOpenAICompletionsParams(model, TranscriptContext{Messages: []Message{
			&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
		}}, options, &compat, retention, nil)
		if err != nil {
			t.Fatalf("BuildOpenAICompletionsParams: %v", err)
		}
		return params
	}
	model := &Model{
		Provider: "openrouter", ID: "reasoning-model", API: APIOpenAICompletions,
		BaseURL: "https://openrouter.ai/api/v1", Reasoning: true,
	}
	params := newParams(model, &OpenAICompletionsOptions{}, CacheRetentionShort)
	if params.ReasoningObj == nil || params.ReasoningObj.Effort == nil || *params.ReasoningObj.Effort != "none" {
		t.Fatalf("missing off mapping: reasoning = %+v; want effort \"none\"", params.ReasoningObj)
	}

	mapped := *model
	mapped.ThinkingLevelMap = map[ThinkingLevel]*string{ThinkOff: new("low")}
	params = newParams(&mapped, &OpenAICompletionsOptions{}, CacheRetentionShort)
	if params.ReasoningObj == nil || params.ReasoningObj.Effort == nil || *params.ReasoningObj.Effort != "low" {
		t.Fatalf("mapped off: reasoning = %+v; want effort \"low\"", params.ReasoningObj)
	}

	disabled := *model
	disabled.ThinkingLevelMap = map[ThinkingLevel]*string{ThinkOff: nil}
	params = newParams(&disabled, &OpenAICompletionsOptions{}, CacheRetentionShort)
	if params.ReasoningObj != nil {
		t.Fatalf("explicit null off must omit reasoning, got %+v", params.ReasoningObj)
	}

	requested := *model
	requested.ThinkingLevelMap = map[ThinkingLevel]*string{ThinkHigh: new("xhigh")}
	params = newParams(&requested, &OpenAICompletionsOptions{ReasoningEffort: ThinkHigh}, CacheRetentionShort)
	if params.ReasoningObj == nil || params.ReasoningObj.Effort == nil || *params.ReasoningObj.Effort != "xhigh" {
		t.Fatalf("requested high: reasoning = %+v; want effort \"xhigh\"", params.ReasoningObj)
	}
}

// pi v1.4.0 api/openai-completions.ts consumes the OpenAI SDK iterator, which
// throws APIError(undefined, data.error, ...) as soon as a chunk carries a
// truthy `error`, before yielding it.
func TestOpenAIStreamFailsOnStreamedErrorPayload(t *testing.T) {
	events := []string{
		"data: {\"id\":\"c1\",\"model\":\"gpt-5\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n",
		"data: {\"error\":{\"message\":\"boom\",\"type\":\"server_error\"}}\n\n",
	}
	server := sseServer(t, events, nil)
	defer server.Close()

	model := testOpenAIModel()
	model.BaseURL = server.URL + "/v1"
	var providerEvents int
	stream := StreamOpenAICompletions(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &OpenAICompletionsOptions{
		APIKey: "test-key",
		OnProviderStreamEvent: func(data json.RawMessage, eventModel *Model) {
			providerEvents++
		}})

	var types []string
	for {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		types = append(types, event.Type)
	}
	if got := strings.Join(types, ","); got != "start,text_start,text_delta,error" {
		t.Fatalf("events = %s", got)
	}
	message, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message.StopReason != StopError || message.ErrorMessage == nil || *message.ErrorMessage != "boom" {
		t.Fatalf("message = %+v", message)
	}
	if providerEvents != 1 {
		t.Fatalf("provider callbacks = %d; the error chunk must not be yielded", providerEvents)
	}
}

// openai-completions.ts normalizeToolCallId: an id from the Responses API is
// "{call_id}|{item_id}" where the item id can be hundreds of characters of
// base64. Replaying into Chat Completions sanitizes both halves, joins them
// when the item id is non-empty, and caps the result at 40 characters with a
// short hash. The tool result must follow the normalized call id, or the
// provider rejects the pair.
func TestConvertOpenAICompletionsMessagesNormalizesPipeToolCallIDs(t *testing.T) {
	model := testOpenAIModel() // provider openai, model gpt-5
	compat := DetectOpenAICompletionsCompat(model)
	base64Item := strings.Repeat("a+/=", 100)

	for _, testCase := range []struct {
		name    string
		id      string
		want    string
		exact   bool
		wantLen int
	}{
		{name: "joined halves", id: "call_1|item_1", want: "call_1_item_1", exact: true},
		{name: "empty item id", id: "call_2|", want: "call_2", exact: true},
		{name: "sanitized characters", id: "call|3+item", want: "call_3_item", exact: true},
		// Both halves are long, so the 40-character cap truncates the call id
		// before the hash; a short call id keeps its whole self plus the hash.
		{name: "capped at forty", id: strings.Repeat("c", 50) + "|" + base64Item, wantLen: 40, exact: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			messages := []Message{
				// The assistant that produced the call ran on a different model,
				// which is when the reference normalizes ids.
				&AssistantMessage{
					Content: ContentList{ToolCall{ID: testCase.id, Name: "bash", Arguments: json.RawMessage(`{}`)}},
					API:     APIOpenAICompletions, Provider: "openai-codex", Model: "codex-1",
					StopReason: StopToolUse, Timestamp: 1,
				},
				&ToolResultMessage{
					ToolCallID: testCase.id, ToolName: "bash",
					Content: UserContentList{TextContent{Text: "out"}}, Timestamp: 2,
				},
			}
			params := ConvertOpenAICompletionsMessages(model, TranscriptContext{Messages: messages}, compat, nil)
			if len(params) != 2 || len(params[0].ToolCalls) != 1 {
				t.Fatalf("params = %+v", params)
			}
			got := params[0].ToolCalls[0].ID
			if testCase.exact {
				if got != testCase.want {
					t.Fatalf("tool call id = %q, want %q", got, testCase.want)
				}
			} else {
				if JSLength(got) != testCase.wantLen {
					t.Fatalf("tool call id = %q (len %d), want %d characters", got, JSLength(got), testCase.wantLen)
				}
				prefix, hash, ok := strings.Cut(got, "_")
				if !ok || len(hash) != 8 || len(prefix) != 31 {
					t.Fatalf("capped id = %q, want 31 sanitized characters and an 8-character hash", got)
				}
				if strings.ContainsAny(hash, "+/=") {
					t.Fatalf("hash %q is not base36", hash)
				}
			}
			if params[1].ToolCallID != got {
				t.Fatalf("tool result id = %q, want the call's %q", params[1].ToolCallID, got)
			}
		})
	}

	// A plain id replayed into an openai provider is cut to 40 characters
	// without the pipe branch (openai-completions.ts `id.length > 40 ?
	// id.slice(0, 40) : id`). A 41-character id is the boundary case.
	longID := strings.Repeat("i", 41)
	params := ConvertOpenAICompletionsMessages(model, TranscriptContext{Messages: []Message{
		&AssistantMessage{
			Content: ContentList{ToolCall{ID: longID, Name: "bash", Arguments: json.RawMessage(`{}`)}},
			API:     APIOpenAICompletions, Provider: "openai", Model: "gpt-4o", StopReason: StopToolUse, Timestamp: 1,
		},
		&ToolResultMessage{ToolCallID: longID, ToolName: "bash", Content: UserContentList{TextContent{Text: "out"}}, Timestamp: 2},
	}}, compat, nil)
	if len(params[0].ToolCalls) != 1 {
		t.Fatalf("params = %+v", params)
	}
	if got := params[0].ToolCalls[0].ID; JSLength(got) != 40 {
		t.Fatalf("openai tool call id = %q (%d chars), want 40", got, JSLength(got))
	}
	if params[1].ToolCallID != params[0].ToolCalls[0].ID {
		t.Fatalf("tool result id = %q, want the call's %q", params[1].ToolCallID, params[0].ToolCalls[0].ID)
	}
}

// Reference: pi v1.5.0 (f9a26365), api/openai-completions.ts emits
// appendGrammarToolInputJsonDelta's JSON wrapper/escapes, not raw custom input.
func TestOpenAICompletionsCustomDeltasFormTheToolArguments(t *testing.T) {
	server := sseServer(t, []string{
		"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","custom":{"name":"expression","input":"print(\""}}]}}]}` + "\n\n",
		"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"custom":{"input":"hi\")\n"}}]}}]}` + "\n\n",
		"data: " + `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n",
	}, nil)
	defer server.Close()
	model := testOpenAIModel()
	model.BaseURL = server.URL
	model.Compat = &ModelCompat{OpenAICompletions: &OpenAICompletionsCompat{SupportsOpenAIGrammarTools: new(true)}}
	transcript := NormalizeContext(Context{Tools: []Tool{{
		Name:       "expression",
		Parameters: json.RawMessage(`{"type":"object","properties":{"payload":{"type":"string"}},"required":["payload"]}`),
		ConstrainedSampling: ConstrainedSamplingValue{Set: true, Config: &ConstrainedSamplingConfig{
			Type: "grammar", Variants: map[string]string{"openai_regex": ".+"},
		}},
	}}})
	stream := StreamOpenAICompletions(model, transcript, &OpenAICompletionsOptions{APIKey: "test-key"})
	var deltas strings.Builder
	for _, event := range stream.Events(context.Background()) {
		if event.Type == EventToolcallDelta {
			deltas.WriteString(event.Delta)
		}
	}
	const want = `{"payload":"print(\"hi\")\n"}`
	if got := deltas.String(); got != want {
		t.Errorf("tool-call delta JSON = %q, want %q", got, want)
	}
	message, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message.StopReason != StopToolUse || len(message.Content) != 1 {
		t.Fatalf("message = %+v, want one finished custom call", message)
	}
	call, ok := message.Content[0].(ToolCall)
	if !ok || string(call.Arguments) != want {
		t.Fatalf("call = %+v, want arguments %s", message.Content[0], want)
	}
}
