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

// google-generative-ai tests keyed to upstream (google-generative-ai.ts +
// google-shared.ts).

func testGoogleModel() *Model {
	return &Model{
		ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", API: APIGoogleGenerativeAI, Provider: "google",
		BaseURL:   "https://generativelanguage.googleapis.com/v1beta",
		Reasoning: true, Input: []string{"text", "image"},
		Cost:          ModelCost{ModelCostRates: ModelCostRates{Input: 0.3, Output: 2.5, CacheRead: 0.075}},
		ContextWindow: 1000000, MaxTokens: 65536,
	}
}

func TestGoogleThinkingLevelHelpers(t *testing.T) {
	// Gemini 3 models use the discrete level control.
	gemini3 := &Model{ID: "gemini-3-pro", Provider: "google", Reasoning: true}
	if !UsesGoogleThinkingLevel(gemini3) {
		t.Fatal("gemini-3-pro should use thinking levels")
	}
	if !UsesGoogleThinkingLevel(&Model{ID: "gemini-3.8-flash"}) ||
		!UsesGoogleThinkingLevel(&Model{ID: "gemini-4-argon"}) ||
		!UsesGoogleThinkingLevel(&Model{ID: "gemini-flash-latest"}) ||
		!UsesGoogleThinkingLevel(&Model{ID: "gemma-4-31b-it"}) {
		t.Fatal("pattern coverage")
	}
	if UsesGoogleThinkingLevel(testGoogleModel()) {
		t.Fatal("2.5-flash uses budgets, not levels")
	}

	// thinkingLevelMap resolution, lowercased.
	model := &Model{ID: "gemini-3-pro", Provider: "google", Reasoning: true,
		ThinkingLevelMap: ThinkingLevelMap{ThinkMinimal: strPtrT("LOW")}}
	if level, err := ResolveGoogleThinkingLevel(model, ThinkMinimal); err != nil || level != "low" {
		t.Fatalf("resolved = %q, %v", level, err)
	}
	// Unsupported mapping errors.
	bad := &Model{ID: "m", Provider: "p", ThinkingLevelMap: ThinkingLevelMap{ThinkHigh: strPtrT("turbo")}}
	if _, err := ResolveGoogleThinkingLevel(bad, ThinkHigh); err == nil {
		t.Fatal("unsupported mapping must error")
	}

	// Budgets per model family.
	if budget := GetGoogleBudget(testGoogleModel(), ThinkMedium, nil); budget != 8192 {
		t.Fatalf("2.5-flash budget = %d", budget)
	}
	if budget := GetGoogleBudget(&Model{ID: "gemini-2.5-pro"}, ThinkHigh, nil); budget != 32768 {
		t.Fatalf("2.5-pro budget = %d", budget)
	}
	if budget := GetGoogleBudget(&Model{ID: "gemini-2.0-flash"}, ThinkHigh, nil); budget != -1 {
		t.Fatalf("unknown family budget = %d; want -1 (dynamic)", budget)
	}
	// Custom budgets win.
	custom := ThinkingBudgets{High: intPtr(1234)}
	if budget := GetGoogleBudget(testGoogleModel(), ThinkHigh, &custom); budget != 1234 {
		t.Fatalf("custom budget = %d", budget)
	}

	// Tool call id requirements.
	if !RequiresToolCallID("claude-sonnet-4") || !RequiresToolCallID("gemini-3-pro") ||
		RequiresToolCallID("gemini-2.5-flash") {
		t.Fatal("tool call id requirements")
	}
	// Strict sampling only on Gemini 3+.
	if !SupportsGoogleStrictToolSampling("gemini-3.1-pro-preview") || SupportsGoogleStrictToolSampling("gemini-2.5-flash") {
		t.Fatal("strict sampling gate")
	}
}

func TestGoogleThoughtSignatureRetention(t *testing.T) {
	// Valid base64 signature retained; invalid dropped.
	if got := resolveThoughtSignature(true, "c2lnbmF0dXJl"); got != "c2lnbmF0dXJl" {
		t.Fatalf("valid signature dropped: %q", got)
	}
	if got := resolveThoughtSignature(true, "not-base64!!"); got != "" {
		t.Fatalf("invalid signature kept: %q", got)
	}
	if got := resolveThoughtSignature(false, "c2lnbmF0dXJl"); got != "" {
		t.Fatalf("cross-model signature kept: %q", got)
	}
	// Length not a multiple of 4 is invalid.
	if got := resolveThoughtSignature(true, "abc"); got != "" {
		t.Fatalf("odd-length signature kept: %q", got)
	}
	// Streaming retention: keep the last non-empty value.
	incoming := "bmV3"
	if got := RetainThoughtSignature("b2xk", &incoming); got != "bmV3" {
		t.Fatalf("retain = %q", got)
	}
	if got := RetainThoughtSignature("b2xk", nil); got != "b2xk" {
		t.Fatalf("retain existing = %q", got)
	}
}

func TestConvertGoogleMessages(t *testing.T) {
	model := testGoogleModel()
	messages := []Message{
		&SystemMessage{Content: StringOrBlocks{Text: "be brief"}, Timestamp: 0},
		&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
		&AssistantMessage{
			Content: ContentList{
				ThinkingContent{Thinking: "pondering", ThinkingSignature: strPtrT("c2lnbmF0dXJl")},
				ToolCall{ID: "call_1", Name: "bash", Arguments: json.RawMessage(`{"cmd":"ls"}`)},
			},
			API: APIGoogleGenerativeAI, Provider: "google", Model: "gemini-2.5-flash",
			StopReason: StopToolUse, Timestamp: 2,
		},
		&ToolResultMessage{
			ToolCallID: "call_1", ToolName: "bash",
			Content: UserContentList{TextContent{Text: "out"}}, Timestamp: 3,
		},
	}
	contents, err := ConvertGoogleMessages(model, TranscriptContext{Messages: messages})
	if err != nil {
		t.Fatal(err)
	}
	// The leading system message is excluded (sent as systemInstruction).
	if len(contents) != 3 {
		t.Fatalf("contents = %d", len(contents))
	}
	if contents[0].Role != "user" || contents[1].Role != "model" || contents[2].Role != "user" {
		t.Fatalf("roles = %s/%s/%s", contents[0].Role, contents[1].Role, contents[2].Role)
	}
	// Same-model thinking stays a thought part with its signature.
	if len(contents[1].Parts) != 2 {
		t.Fatalf("model parts = %+v", contents[1].Parts)
	}
	thought := contents[1].Parts[0]
	if thought.Thought == nil || !*thought.Thought || thought.ThoughtSignature == nil || *thought.ThoughtSignature != "c2lnbmF0dXJl" {
		t.Fatalf("thought part = %+v", thought)
	}
	if contents[1].Parts[1].FunctionCall == nil || contents[1].Parts[1].FunctionCall.Name != "bash" {
		t.Fatalf("function call part = %+v", contents[1].Parts[1])
	}
	// Tool result → functionResponse with the output key.
	response := contents[2].Parts[0].FunctionResponse
	if response == nil || response.Name != "bash" || !strings.Contains(string(response.Response), `"output":"out"`) {
		t.Fatalf("function response = %+v", response)
	}

	// Consecutive tool results merge into one user turn.
	messages = append(messages, &ToolResultMessage{
		ToolCallID: "call_2", ToolName: "bash",
		Content: UserContentList{TextContent{Text: "out2"}}, Timestamp: 4,
	})
	contents, err = ConvertGoogleMessages(model, TranscriptContext{Messages: messages})
	if err != nil {
		t.Fatal(err)
	}
	last := contents[len(contents)-1]
	if last.Role != "user" || len(last.Parts) != 2 {
		t.Fatalf("merged turn = %+v", last)
	}
}

// Upstream google-shared.ts sanitizes IDs with a non-Unicode regex over UTF-16 code units.
func TestConvertGoogleMessagesNormalizesAstralToolCallIDLikeJavaScript(t *testing.T) {
	model := testGoogleModel()
	model.ID = "gemini-3-pro"
	messages := []Message{&AssistantMessage{
		Content: ContentList{ToolCall{ID: "call|item😀", Name: "read", Arguments: json.RawMessage(`{}`)}},
		API:     APIGoogleGenerativeAI, Provider: model.Provider, Model: "gemini-2.5-flash",
		StopReason: StopToolUse,
	}}
	contents, err := ConvertGoogleMessages(model, TranscriptContext{Messages: messages})
	if err != nil {
		t.Fatal(err)
	}
	got := contents[0].Parts[0].FunctionCall.ID
	if got != "call_item__" {
		t.Fatalf("normalized id = %q; want JS UTF-16 replacement result %q", got, "call_item__")
	}
}

func TestConvertGoogleMessagesCrossModelThinking(t *testing.T) {
	model := testGoogleModel()
	// Thinking from a DIFFERENT model is dropped entirely (v1.4.0 #11000), so an
	// assistant message that contains only thinking has no content left.
	messages := []Message{
		&AssistantMessage{
			Content: ContentList{
				ThinkingContent{Thinking: "other model reasoning", ThinkingSignature: strPtrT("c2lnbmF0dXJl")},
			},
			API: APIGoogleGenerativeAI, Provider: "google", Model: "gemini-3-pro",
			StopReason: StopStop, Timestamp: 1,
		},
	}
	contents, err := ConvertGoogleMessages(model, TranscriptContext{Messages: messages})
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 0 {
		t.Fatalf("cross-model thinking was not dropped: %+v", contents)
	}
}

func TestBuildGoogleParams(t *testing.T) {
	model := testGoogleModel()
	tool := Tool{Name: "read_file", Description: "Reads", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}
	ctx := NormalizeContext(Context{
		SystemPrompt: strPtrT("sys"),
		Messages:     []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
		Tools:        []Tool{tool},
	})
	budget := 4096
	params, err := BuildGoogleParams(model, ctx, &GoogleOptions{
		StreamOptions: StreamOptions{Temperature: floatPtrT(0.5)},
		Thinking:      &GoogleThinkingOption{Enabled: true, BudgetTokens: &budget},
	})
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
	if !strings.Contains(string(probe["systemInstruction"]), `"text":"sys"`) {
		t.Fatalf("systemInstruction = %s", probe["systemInstruction"])
	}
	if !strings.Contains(string(probe["tools"]), `"parametersJsonSchema"`) {
		t.Fatalf("tools = %s", probe["tools"])
	}
	if !strings.Contains(string(probe["generationConfig"]), `"thinkingBudget":4096`) ||
		!strings.Contains(string(probe["generationConfig"]), `"includeThoughts":true`) {
		t.Fatalf("generationConfig = %s", probe["generationConfig"])
	}
	if !strings.Contains(string(probe["generationConfig"]), `"temperature":0.5`) {
		t.Fatalf("temperature missing: %s", probe["generationConfig"])
	}
}

func TestMapGoogleStopReasonAndToolChoice(t *testing.T) {
	if MapGoogleStopReason("STOP") != StopStop || MapGoogleStopReason("MAX_TOKENS") != StopLength {
		t.Fatal("basic mapping")
	}
	for _, safety := range []string{"SAFETY", "RECITATION", "MALFORMED_FUNCTION_CALL"} {
		if MapGoogleStopReason(safety) != StopError {
			t.Fatalf("%s should map to error", safety)
		}
	}
	if MapGoogleToolChoice("any") != "ANY" || MapGoogleToolChoice("none") != "NONE" || MapGoogleToolChoice("auto") != "AUTO" {
		t.Fatal("tool choice mapping")
	}
	// Strict tools force VALIDATED mode; explicit none/any win.
	strictTool := Tool{Name: "t", Description: "", Parameters: json.RawMessage(`{"type":"object","properties":{}}`),
		ConstrainedSampling: ConstrainedSamplingValue{Set: true, Config: &ConstrainedSamplingConfig{Type: "json_schema", Strict: "prefer"}}}
	plainTool := Tool{Name: "p", Description: "", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}
	if mode := ResolveGoogleFunctionCallingMode([]Tool{strictTool}, "", true); mode != "VALIDATED" {
		t.Fatalf("strict mode = %q", mode)
	}
	if mode := ResolveGoogleFunctionCallingMode([]Tool{plainTool}, "", true); mode != "" {
		t.Fatalf("plain mode = %q", mode)
	}
	if mode := ResolveGoogleFunctionCallingMode([]Tool{strictTool}, "none", true); mode != "NONE" {
		t.Fatalf("none mode = %q", mode)
	}
}

// Mirrors streamed text-block accumulation in
// packages/ai/src/api/google-generative-ai.ts.
func TestStreamGoogleTextDeltasAvoidPrefixCopies(t *testing.T) {
	const chunks = 512
	delta := strings.Repeat("x", 128)
	var body strings.Builder
	for index := 0; index < chunks; index++ {
		finish := ""
		if index == chunks-1 {
			finish = `,"finishReason":"STOP"`
		}
		body.WriteString(`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"`)
		body.WriteString(delta)
		body.WriteString(`"}]}` + finish + `}]}` + "\n\n")
	}
	bodyText := body.String()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodyText))
	}))
	defer server.Close()

	model := testGoogleModel()
	model.BaseURL = server.URL
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	message, err := StreamGoogleGenerativeAI(model, NormalizeContext(Context{}), &GoogleOptions{
		StreamOptions: StreamOptions{APIKey: "k"},
	}).Result(context.Background())
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("stream result: %v", err)
	}
	want := strings.Repeat(delta, chunks)
	if len(message.Content) != 1 {
		t.Fatalf("content = %+v, want one block", message.Content)
	}
	if text, ok := message.Content[0].(TextContent); !ok || text.Text != want {
		t.Fatalf("text = %T length %d, want %d bytes", message.Content[0], len(text.Text), len(want))
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("streaming %d bytes of Google text allocated %d bytes", len(want), allocated)
	if allocated > uint64(len(want))*64 {
		t.Fatalf("streaming %d bytes allocated %d bytes, want less than %d", len(want), allocated, len(want)*64)
	}
}

func TestStreamGoogleGenerativeAI(t *testing.T) {
	chunks := []string{
		`{"responseId":"resp_1","candidates":[{"content":{"role":"model","parts":[{"text":"think","thought":true,"thoughtSignature":"c2lnbmF0dXJl"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"ing","thought":true}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"bash","args":{"cmd":"ls"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":10,"thoughtsTokenCount":5,"cachedContentTokenCount":40,"totalTokenCount":115}}`,
		`[DONE]`,
	}
	var chunksOut []string
	for _, chunk := range chunks {
		chunksOut = append(chunksOut, "data: "+chunk+"\n\n")
	}
	var capturedPath, capturedKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedKey = r.Header.Get("x-goog-api-key")
		for _, chunk := range chunksOut {
			_, _ = w.Write([]byte(chunk))
		}
	}))
	defer server.Close()

	model := testGoogleModel()
	model.BaseURL = server.URL
	var providerEventCount int
	stream := StreamGoogleGenerativeAI(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &GoogleOptions{StreamOptions: StreamOptions{
		APIKey: "k",
		OnProviderStreamEvent: func(data json.RawMessage, eventModel *Model) {
			if eventModel != model || !json.Valid(data) {
				t.Errorf("provider callback data/model = %s/%p", data, eventModel)
			}
			providerEventCount++
		},
	}})

	var types []string
	var thinking, text string
	for {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		types = append(types, event.Type)
		if event.Type == EventThinkingDelta {
			thinking += event.Delta
		}
		if event.Type == EventTextDelta {
			text += event.Delta
		}
	}
	if thinking != "thinking" || text != "Hello" {
		t.Fatalf("thinking=%q text=%q", thinking, text)
	}
	if capturedKey != "k" || !strings.Contains(capturedPath, ":streamGenerateContent") {
		t.Fatalf("request = %s key=%s", capturedPath, capturedKey)
	}
	msg, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Tool call upgrades STOP → toolUse.
	if msg.StopReason != StopToolUse {
		t.Fatalf("stopReason = %s", msg.StopReason)
	}
	if providerEventCount != 4 {
		t.Fatalf("provider callback count = %d; want 4", providerEventCount)
	}
	// Usage: input excludes cached tokens; output includes thoughts.
	if msg.Usage.Input != 60 || msg.Usage.Output != 15 || msg.Usage.CacheRead != 40 || msg.Usage.TotalTokens != 115 {
		t.Fatalf("usage = %+v", msg.Usage)
	}
	if msg.Usage.Reasoning == nil || *msg.Usage.Reasoning != 5 {
		t.Fatalf("reasoning = %v", msg.Usage.Reasoning)
	}
	// The thinking block retains its signature from the first delta.
	var thoughtPart, textPart *ThinkingContent
	for _, block := range msg.Content {
		if thinking, ok := block.(ThinkingContent); ok {
			copy := thinking
			thoughtPart = &copy
		}
	}
	if thoughtPart == nil || thoughtPart.ThinkingSignature == nil || *thoughtPart.ThinkingSignature != "c2lnbmF0dXJl" {
		t.Fatalf("thinking = %+v", thoughtPart)
	}
	_ = textPart
	// The stream started with the responseId.
	if msg.ResponseID == nil || *msg.ResponseID != "resp_1" {
		t.Fatalf("responseId = %v", msg.ResponseID)
	}
}

func TestStreamGoogleGenerativeAIErrors(t *testing.T) {
	t.Run("missing api key", func(t *testing.T) {
		stream := StreamGoogleGenerativeAI(testGoogleModel(), NormalizeContext(Context{}), &GoogleOptions{})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopError || *msg.ErrorMessage != "No API key for provider: google" {
			t.Fatalf("message = %+v", msg)
		}
	})
	t.Run("safety finish reason", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"x\"}]},\"finishReason\":\"SAFETY\"}]}\n\n"))
		}))
		defer server.Close()
		model := testGoogleModel()
		model.BaseURL = server.URL
		stream := StreamGoogleGenerativeAI(model, NormalizeContext(Context{}), &GoogleOptions{StreamOptions: StreamOptions{APIKey: "k"}})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopError || !strings.Contains(*msg.ErrorMessage, "Provider stopped with: SAFETY") {
			t.Fatalf("message = %+v", msg)
		}
	})
	t.Run("no finish reason", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"x\"}]}}]}\n\n"))
		}))
		defer server.Close()
		model := testGoogleModel()
		model.BaseURL = server.URL
		stream := StreamGoogleGenerativeAI(model, NormalizeContext(Context{}), &GoogleOptions{StreamOptions: StreamOptions{APIKey: "k"}})
		msg, _ := stream.Result(context.Background())
		if msg.StopReason != StopError || !strings.Contains(*msg.ErrorMessage, "without a finish reason") {
			t.Fatalf("message = %+v", msg)
		}
	})
}

// A whitespace-only tool result counts as no text, so the function response
// carries the empty value (or the image placeholder) instead of the whitespace,
// which some providers reject. Upstream v1.4.0 4dd2b149d (issue #8720) changed
// google-shared.ts's hasText to `textResult.trim().length > 0`.
func TestConvertGoogleMessagesWhitespaceToolResultHasNoText(t *testing.T) {
	model := testGoogleModel()
	for _, text := range []string{"", " ", "\r\n", "\n\n\t ", "\u00A0", "\uFEFF"} {
		messages := []Message{&ToolResultMessage{
			ToolCallID: "call_1", ToolName: "bash",
			Content: UserContentList{TextContent{Text: text}}, Timestamp: 1,
		}}
		contents, err := ConvertGoogleMessages(model, TranscriptContext{Messages: messages})
		if err != nil {
			t.Fatal(err)
		}
		response := contents[0].Parts[0].FunctionResponse
		if response == nil || string(response.Response) != `{"output":""}` {
			t.Fatalf("function response for %q = %+v; want an empty output", text, response)
		}
	}
	// Visible text travels verbatim, surrounding whitespace included.
	messages := []Message{&ToolResultMessage{
		ToolCallID: "call_1", ToolName: "bash",
		Content: UserContentList{TextContent{Text: " \r\nout\r\n "}}, Timestamp: 1,
	}}
	contents, err := ConvertGoogleMessages(model, TranscriptContext{Messages: messages})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(contents[0].Parts[0].FunctionResponse.Response); got != `{"output":" \r\nout\r\n "}` {
		t.Fatalf("function response = %s; want the text verbatim", got)
	}
}
