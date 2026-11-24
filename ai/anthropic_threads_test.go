package ai

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Port of pi v1.5.2 (b4afdddf) test/anthropic-message-threads.test.ts.
// The fake endpoint records request bodies and beta lists and answers from a
// queue or with a plain reply; like the real server it rejects a continue whose
// fixed fields differ from the thread's.

const threadTestThreadsBeta = "message-threads-2026-08-12"

// threadTestCaptured is one request the fake endpoint received.
type threadTestCaptured struct {
	// ID is the reply id the default responder uses (msg_<request number>).
	ID    string
	Body  map[string]any
	Betas []string
}

func (c threadTestCaptured) thread() map[string]any {
	thread, _ := c.Body["thread"].(map[string]any)
	return thread
}

func (c threadTestCaptured) threadType() string {
	if thread := c.thread(); thread != nil {
		value, _ := thread["type"].(string)
		return value
	}
	return ""
}

func (c threadTestCaptured) previousMessageID() string {
	if thread := c.thread(); thread != nil {
		value, _ := thread["previous_message_id"].(string)
		return value
	}
	return ""
}

func (c threadTestCaptured) has(key string) bool {
	_, ok := c.Body[key]
	return ok
}

func (c threadTestCaptured) messages() []any {
	list, _ := c.Body["messages"].([]any)
	return list
}

// threadTestReply is one fake HTTP answer.
type threadTestReply struct {
	Status  int
	Body    string
	Headers map[string]string
	JSON    bool
}

type threadTestToolCall struct {
	ID    string
	Name  string
	Input map[string]any
}

// threadTestReplySpec scripts a fake streamed reply.
type threadTestReplySpec struct {
	ID         string
	Model      string
	Text       *string
	Thinking   string
	ToolCalls  []threadTestToolCall
	StopReason string
	// End replaces the normal ending: "error_event" or "no_message_stop".
	End string
}

// threadTestSSE builds the SSE body of one reply.
func threadTestSSE(request threadTestCaptured, spec threadTestReplySpec) threadTestReply {
	id := spec.ID
	if id == "" {
		id = request.ID
	}
	model, _ := request.Body["model"].(string)
	if spec.Model != "" {
		model = spec.Model
	}
	text := "answer"
	if spec.Text != nil {
		text = *spec.Text
	}
	var events []string
	add := func(typ string, payload map[string]any) {
		payload["type"] = typ
		events = append(events, "event: "+typ+"\ndata: "+string(mustMarshalJSON(payload))+"\n\n")
	}
	add("message_start", map[string]any{"message": map[string]any{
		"id": id, "model": model, "usage": map[string]any{"input_tokens": 10, "output_tokens": 0},
	}})
	index := 0
	if spec.Thinking != "" {
		add("content_block_start", map[string]any{
			"index": index, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""},
		})
		add("content_block_delta", map[string]any{
			"index": index, "delta": map[string]any{"type": "thinking_delta", "thinking": spec.Thinking},
		})
		add("content_block_delta", map[string]any{
			"index": index, "delta": map[string]any{"type": "signature_delta", "signature": "sig"},
		})
		add("content_block_stop", map[string]any{"index": index})
		index++
	}
	add("content_block_start", map[string]any{
		"index": index, "content_block": map[string]any{"type": "text", "text": ""},
	})
	add("content_block_delta", map[string]any{
		"index": index, "delta": map[string]any{"type": "text_delta", "text": text},
	})
	add("content_block_stop", map[string]any{"index": index})
	index++
	for _, call := range spec.ToolCalls {
		add("content_block_start", map[string]any{
			"index": index, "content_block": map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": map[string]any{}},
		})
		arguments, err := json.Marshal(call.Input)
		if err != nil {
			panic(err)
		}
		add("content_block_delta", map[string]any{
			"index": index, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(arguments)},
		})
		add("content_block_stop", map[string]any{"index": index})
		index++
	}
	if spec.End == "error_event" {
		events = append(events, `event: error`+"\n"+`data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`+"\n\n")
		return threadTestReply{Body: strings.Join(events, "")}
	}
	stopReason := spec.StopReason
	if stopReason == "" {
		stopReason = "end_turn"
		if len(spec.ToolCalls) > 0 {
			stopReason = "tool_use"
		}
	}
	add("message_delta", map[string]any{
		"delta": map[string]any{"stop_reason": stopReason}, "usage": map[string]any{"output_tokens": 1},
	})
	if spec.End != "no_message_stop" {
		add("message_stop", map[string]any{})
	}
	return threadTestReply{Body: strings.Join(events, "")}
}

// threadTestErrorReply builds an Anthropic error response, with the
// `error.details.error_code` the thread rejections use.
func threadTestErrorReply(status int, message, errorCode string, headers map[string]string) threadTestReply {
	errorType := "invalid_request_error"
	switch status {
	case http.StatusNotFound:
		errorType = "not_found_error"
	case http.StatusTooManyRequests:
		errorType = "rate_limit_error"
	}
	body := map[string]any{"type": "error", "error": map[string]any{"type": errorType, "message": message}}
	if errorCode != "" {
		body["error"].(map[string]any)["details"] = map[string]any{"error_code": errorCode}
	}
	return threadTestReply{Status: status, Body: string(mustMarshalJSON(body)), Headers: headers, JSON: true}
}

// threadTestHarness is the fake Anthropic endpoint.
type threadTestHarness struct {
	mu           sync.Mutex
	requests     []threadTestCaptured
	queue        []func(threadTestCaptured) threadTestReply
	fingerprints map[string]string
	sessionID    string
}

var threadTestSessionCounter atomic.Int64

func newThreadTestHarness(t *testing.T) *threadTestHarness {
	t.Helper()
	h := &threadTestHarness{
		sessionID:    fmt.Sprintf("thread-test-%d", threadTestSessionCounter.Add(1)),
		fingerprints: map[string]string{},
	}
	server := httptest.NewServer(http.HandlerFunc(h.handle))
	t.Cleanup(func() {
		server.Close()
		// Upstream's afterEach calls cleanupSessionResources().
		ResetAnthropicMessageThreads("")
		ResetStrictToolLimits("")
	})
	routeThreadTestRequests(t, server.URL)
	return h
}

// routeThreadTestRequests sends every provider request to the fake server while
// the model keeps upstream's https://api.anthropic.com base URL, so the tests
// exercise the endpoint-derived thread default instead of an injected fetch.
func routeThreadTestRequests(t *testing.T, serverURL string) {
	t.Helper()
	target, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	http.DefaultTransport = &threadTestTransport{base: original, target: target}
	t.Cleanup(func() { http.DefaultTransport = original })
}

type threadTestTransport struct {
	base   http.RoundTripper
	target *url.URL
}

func (t *threadTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	next := request.Clone(request.Context())
	next.URL.Scheme = t.target.Scheme
	next.URL.Host = t.target.Host
	next.Host = t.target.Host
	return t.base.RoundTrip(next)
}

func splitThreadTestBetas(value string) []string {
	var betas []string
	for beta := range strings.SplitSeq(value, ",") {
		if beta = strings.TrimSpace(beta); beta != "" {
			betas = append(betas, beta)
		}
	}
	return betas
}

func (h *threadTestHarness) handle(w http.ResponseWriter, request *http.Request) {
	raw, _ := io.ReadAll(request.Body)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	captured := threadTestCaptured{Body: body, Betas: splitThreadTestBetas(request.Header.Get("anthropic-beta"))}
	h.mu.Lock()
	captured.ID = fmt.Sprintf("msg_%d", len(h.requests)+1)
	h.requests = append(h.requests, captured)
	var responder func(threadTestCaptured) threadTestReply
	if len(h.queue) > 0 {
		responder = h.queue[0]
		h.queue = h.queue[1:]
	}
	h.mu.Unlock()

	reply := threadTestReply{}
	if responder != nil {
		reply = responder(captured)
	} else {
		reply = h.defaultReply(captured)
	}
	status := cmp.Or(reply.Status, http.StatusOK)
	for name, value := range reply.Headers {
		w.Header().Set(name, value)
	}
	if reply.JSON {
		w.Header().Set("Content-Type", "application/json")
	} else {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, reply.Body)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// threadTestFingerprint is the fields Anthropic requires to stay the same
// within a thread (observed live).
func threadTestFingerprint(request threadTestCaptured) string {
	fallbacks, hasFallbacks := request.Body["fallbacks"]
	values := []any{
		request.Body["model"], request.Body["thinking"], request.Body["output_config"],
		request.Body["tool_choice"], hasFallbacks && fallbacks != nil, request.Betas,
	}
	return string(mustMarshalJSON(values))
}

func (h *threadTestHarness) defaultReply(request threadTestCaptured) threadTestReply {
	h.mu.Lock()
	defer h.mu.Unlock()
	expected := h.fingerprints[request.previousMessageID()]
	if request.thread() == nil || request.previousMessageID() == "" {
		expected = threadTestFingerprint(request)
	}
	if expected != threadTestFingerprint(request) {
		return threadTestErrorReply(http.StatusBadRequest, "fixed field changed", "thread_fingerprint_mismatch", nil)
	}
	if request.thread() != nil {
		h.fingerprints[request.ID] = expected
	}
	return threadTestSSE(request, threadTestReplySpec{ID: request.ID})
}

func (h *threadTestHarness) push(responder func(threadTestCaptured) threadTestReply) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.queue = append(h.queue, responder)
}

func (h *threadTestHarness) snapshot() []threadTestCaptured {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]threadTestCaptured(nil), h.requests...)
}

func (h *threadTestHarness) request(t *testing.T, index int) threadTestCaptured {
	t.Helper()
	requests := h.snapshot()
	if index >= len(requests) {
		t.Fatalf("request %d missing, %d sent", index, len(requests))
	}
	return requests[index]
}

func (h *threadTestHarness) count() int { return len(h.snapshot()) }

// threadTestModel is upstream's baseModel: provider anthropic on Anthropic's
// own endpoint, so message threads are on by default.
func threadTestModel() *Model {
	return &Model{
		ID: "claude-test", Name: "Test Claude", API: APIAnthropicMessages, Provider: "anthropic",
		BaseURL: "https://api.anthropic.com", Input: []string{"text"},
		ContextWindow: 200000, MaxTokens: 100,
	}
}

func threadTestTool(name string) Tool {
	return Tool{
		Name: name, Description: "Tool " + name,
		Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`),
	}
}

func threadTestUser(text string) Message {
	return &UserMessage{Content: StringOrBlocks{Text: text}, Timestamp: 1}
}

func threadTestToolResult(id, name string) Message {
	return &ToolResultMessage{
		ToolCallID: id, ToolName: name, Content: UserContentList{TextContent{Text: "result"}}, Timestamp: 1,
	}
}

// threadTurn is one turn's inputs; a zero field keeps the harness default.
type threadTurn struct {
	Model        *Model
	SystemPrompt string
	Tools        []Tool
	Options      func(*AnthropicOptions)
}

func (h *threadTestHarness) options() *AnthropicOptions {
	zeroRetries := 0
	return &AnthropicOptions{
		APIKey: "test-key", SessionID: h.sessionID, MaxRetries: &zeroRetries}
}

func (h *threadTestHarness) turn(t *testing.T, messages []Message, input threadTurn) *AssistantMessage {
	t.Helper()
	model := threadTestModel()
	if input.Model != nil {
		model = input.Model
	}
	prompt := "You are a test assistant."
	if input.SystemPrompt != "" {
		prompt = input.SystemPrompt
	}
	tools := []Tool{threadTestTool("lookup")}
	if input.Tools != nil {
		tools = input.Tools
	}
	options := h.options()
	if input.Options != nil {
		input.Options(options)
	}
	resolved := NormalizeContext(Context{SystemPrompt: &prompt, Tools: tools, Messages: messages})
	message, err := StreamAnthropic(model, resolved, options).Result(context.Background())
	if err != nil {
		t.Fatalf("stream result: %v", err)
	}
	return message
}

// say runs one turn and appends the reply to the conversation.
func (h *threadTestHarness) say(t *testing.T, messages []Message, input threadTurn) ([]Message, *AssistantMessage) {
	t.Helper()
	result := h.turn(t, messages, input)
	return append(messages, result), result
}

func requireThreadJSON(t *testing.T, got any, want string) {
	t.Helper()
	var expected any
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatalf("bad expectation %q: %v", want, err)
	}
	gotEncoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantEncoded, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotEncoded) != string(wantEncoded) {
		t.Fatalf("got %s, want %s", gotEncoded, wantEncoded)
	}
}

func requireThreadType(t *testing.T, request threadTestCaptured, want string) {
	t.Helper()
	if want == "" {
		if thread := request.thread(); thread != nil {
			t.Fatalf("thread = %v, want none", thread)
		}
		return
	}
	if got := request.threadType(); got != want {
		t.Fatalf("thread type = %q, want %q", got, want)
	}
}

func requireThreadFallbackDiagnostic(t *testing.T, message *AssistantMessage, wantReason string) {
	t.Helper()
	if len(message.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", message.Diagnostics)
	}
	diagnostic := message.Diagnostics[0]
	if diagnostic.Type != "anthropic_thread_fallback" {
		t.Fatalf("diagnostic type = %q", diagnostic.Type)
	}
	requireThreadJSON(t, diagnostic.Details, fmt.Sprintf(`{"reason":%q}`, wantReason))
}

func threadTypes(requests []threadTestCaptured) []string {
	types := make([]string, 0, len(requests))
	for _, request := range requests {
		types = append(types, request.threadType())
	}
	return types
}

// Upstream test: "creates a thread, then continues it with only the new messages".
func TestAnthropicMessageThreadsCreatesThenContinues(t *testing.T) {
	h := newThreadTestHarness(t)
	messages := []Message{threadTestUser("first")}
	messages, _ = h.say(t, messages, threadTurn{})
	messages = append(messages, threadTestUser("second"))
	messages, result := h.say(t, messages, threadTurn{})
	_ = messages

	if result.StopReason != StopStop {
		t.Fatalf("stop reason = %q", result.StopReason)
	}
	create, next := h.request(t, 0), h.request(t, 1)
	requireThreadJSON(t, create.thread(), `{"type":"create"}`)
	if !create.has("system") || !create.has("tools") {
		t.Fatal("create request lacks system or tools")
	}
	requireThreadJSON(t, next.thread(), `{"type":"continue","previous_message_id":"msg_1"}`)
	if next.has("system") || next.has("tools") {
		t.Fatal("continue request repeated system or tools")
	}
	// The first user message had string content turned into a cached block on
	// request 1, and plain string content on request 2; the cache marker moved
	// to the new message.
	requireThreadJSON(t, create.messages()[0], `{"role":"user","content":[{"type":"text","text":"first","cache_control":{"type":"ephemeral"}}]}`)
	requireThreadJSON(t, next.messages(), `[{"role":"user","content":[{"type":"text","text":"second","cache_control":{"type":"ephemeral"}}]}]`)
	if !slices.Contains(create.Betas, threadTestThreadsBeta) {
		t.Fatalf("create betas = %v", create.Betas)
	}
	if !slices.Equal(create.Betas, next.Betas) {
		t.Fatalf("next betas = %v, want %v", next.Betas, create.Betas)
	}
}

// Upstream test: "continues after a reply that ends in tool calls, without
// placeholder results in the prefix".
func TestAnthropicMessageThreadsContinuesAfterToolCalls(t *testing.T) {
	h := newThreadTestHarness(t)
	h.push(func(request threadTestCaptured) threadTestReply {
		return threadTestSSE(request, threadTestReplySpec{
			ID: "msg_tool", ToolCalls: []threadTestToolCall{{ID: "toolu_1", Name: "lookup", Input: map[string]any{"value": "x"}}},
		})
	})
	messages := []Message{threadTestUser("look it up")}
	messages, first := h.say(t, messages, threadTurn{})
	if first.StopReason != StopToolUse {
		t.Fatalf("stop reason = %q", first.StopReason)
	}
	messages = append(messages, threadTestToolResult("toolu_1", "lookup"))
	h.say(t, messages, threadTurn{})

	next := h.request(t, 1)
	requireThreadJSON(t, next.thread(), `{"type":"continue","previous_message_id":"msg_tool"}`)
	// No placeholder results for the trailing tool calls in the prefix, so the
	// new messages are just the tool result.
	if len(next.messages()) != 1 {
		t.Fatalf("messages = %v", next.messages())
	}
	requireThreadJSON(t, next.messages()[0], `{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"result","is_error":false,"cache_control":{"type":"ephemeral"}}]}`)
}

// Upstream test: "continues when the effort level changes between turns".
func TestAnthropicMessageThreadsContinueAcrossEffortChanges(t *testing.T) {
	model := threadTestModel()
	model.Reasoning = true
	model.Compat = &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{
		ForceAdaptiveThinking: new(true), SupportsMidConvoEffort: new(true),
	}}
	h := newThreadTestHarness(t)
	h.push(func(request threadTestCaptured) threadTestReply {
		return threadTestSSE(request, threadTestReplySpec{ID: "msg_a", Thinking: "thought"})
	})
	messages := []Message{threadTestUser("first")}
	messages, _ = h.say(t, messages, threadTurn{Model: model, Options: func(options *AnthropicOptions) {
		options.ThinkingEnabled = new(true)
		options.Effort = AnthropicEffortHigh
	}})
	messages = append(messages, threadTestUser("second"))
	h.say(t, messages, threadTurn{Model: model, Options: func(options *AnthropicOptions) {
		options.ThinkingEnabled = new(true)
		options.Effort = AnthropicEffortLow
	}})

	efforts := func(request threadTestCaptured) []string {
		out := make([]string, 0, len(request.messages()))
		for _, message := range request.messages() {
			entry, _ := message.(map[string]any)
			if effort, ok := entry["output_config"].(map[string]any); ok {
				value, _ := effort["effort"].(string)
				out = append(out, value)
				continue
			}
			role, _ := entry["role"].(string)
			out = append(out, role)
		}
		return out
	}
	if got, want := efforts(h.request(t, 0)), []string{"user", "high"}; !slices.Equal(got, want) {
		t.Fatalf("first request = %v, want %v", got, want)
	}
	next := h.request(t, 1)
	requireThreadJSON(t, next.thread(), `{"type":"continue","previous_message_id":"msg_a"}`)
	if got, want := efforts(next), []string{"user", "low"}; !slices.Equal(got, want) {
		t.Fatalf("next request = %v, want %v", got, want)
	}
}

// Upstream test: "continues when a system message with a native tool change
// follows the reply".
func TestAnthropicMessageThreadsContinueAcrossNativeToolChanges(t *testing.T) {
	model := threadTestModel()
	model.Compat = &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{
		SupportsMidConvoSystemMessages: new(true), SupportsMidConvoToolChanges: new(true),
	}}
	h := newThreadTestHarness(t)
	messages := []Message{threadTestUser("first")}
	messages, _ = h.say(t, messages, threadTurn{Model: model})
	messages = append(messages,
		&SystemMessage{Content: StringOrBlocks{Text: "Update"}, ToolsAdded: []Tool{threadTestTool("other")}, Timestamp: 2},
		threadTestUser("second"),
	)
	h.say(t, messages, threadTurn{Model: model})

	next := h.request(t, 1)
	requireThreadType(t, next, "continue")
	roles := make([]string, 0, len(next.messages()))
	for _, message := range next.messages() {
		entry, _ := message.(map[string]any)
		role, _ := entry["role"].(string)
		roles = append(roles, role)
	}
	if !slices.Equal(roles, []string{"user", "system"}) {
		t.Fatalf("roles = %v", roles)
	}
	encoded, _ := json.Marshal(next.messages()[1])
	if !strings.Contains(string(encoded), "tool_addition") {
		t.Fatalf("system message = %s", encoded)
	}
}

// Upstream test: "creates a new thread when the %s changes" (system prompt,
// tools, model, tool choice, cache retention, thinking level).
func TestAnthropicMessageThreadsStartNewThreadWhenSettingsChange(t *testing.T) {
	reasoning := threadTestModel()
	reasoning.Reasoning = true
	thinkingTurn := func(budget int) threadTurn {
		return threadTurn{Model: reasoning, Options: func(options *AnthropicOptions) {
			options.ThinkingEnabled = new(true)
			options.ThinkingBudgetTokens = new(budget)
		}}
	}
	cases := []struct {
		name   string
		before threadTurn
		after  threadTurn
	}{
		{"system prompt", threadTurn{}, threadTurn{SystemPrompt: "Different prompt."}},
		{"tools", threadTurn{}, threadTurn{Tools: []Tool{threadTestTool("lookup"), threadTestTool("other")}}},
		{"model", threadTurn{}, func() threadTurn {
			other := threadTestModel()
			other.ID = "claude-other"
			return threadTurn{Model: other}
		}()},
		{"tool choice", threadTurn{}, threadTurn{Options: func(options *AnthropicOptions) {
			options.ToolChoice = mustMarshalJSON("any")
		}}},
		{"cache retention", threadTurn{}, threadTurn{Options: func(options *AnthropicOptions) {
			options.CacheRetention = CacheRetentionLong
		}}},
		{"thinking level", thinkingTurn(1024), thinkingTurn(2048)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			h := newThreadTestHarness(t)
			messages := []Message{threadTestUser("first")}
			messages, _ = h.say(t, messages, testCase.before)
			messages = append(messages, threadTestUser("second"))
			h.say(t, messages, testCase.after)
			next := h.request(t, 1)
			requireThreadType(t, next, "create")
			if len(next.messages()) != 3 {
				t.Fatalf("messages = %v", next.messages())
			}
		})
	}
}

// Upstream test: "starts a new thread with the full request after %s" for each
// NEW_THREAD_ERRORS code.
func TestAnthropicMessageThreadsStartNewThreadAfterContinueRejection(t *testing.T) {
	cases := []struct {
		errorCode string
		status    int
	}{
		{"thread_not_found", http.StatusNotFound},
		{"thread_already_continued", http.StatusBadRequest},
		{"thread_continue_in_flight", http.StatusBadRequest},
		{"thread_fingerprint_mismatch", http.StatusBadRequest},
	}
	for _, testCase := range cases {
		t.Run(testCase.errorCode, func(t *testing.T) {
			h := newThreadTestHarness(t)
			messages := []Message{threadTestUser("first")}
			messages, _ = h.say(t, messages, threadTurn{})
			messages = append(messages, threadTestUser("second"))
			h.push(func(threadTestCaptured) threadTestReply {
				return threadTestErrorReply(testCase.status, "thread rejected", testCase.errorCode, nil)
			})
			onPayloadCalls := 0
			messages, result := h.say(t, messages, threadTurn{Options: func(options *AnthropicOptions) {
				options.OnPayload = func(payload json.RawMessage, model *Model) json.RawMessage {
					onPayloadCalls++
					return nil
				}
			}})

			if result.StopReason != StopStop {
				t.Fatalf("stop reason = %q", result.StopReason)
			}
			if onPayloadCalls != 1 {
				t.Fatalf("onPayload calls = %d", onPayloadCalls)
			}
			if h.count() != 3 {
				t.Fatalf("requests = %d", h.count())
			}
			rejected, resend := h.request(t, 1), h.request(t, 2)
			requireThreadType(t, rejected, "continue")
			requireThreadJSON(t, resend.thread(), `{"type":"create"}`)
			if len(resend.messages()) != 3 || !resend.has("system") {
				t.Fatalf("resend = %v", resend.Body)
			}
			if !slices.Equal(resend.Betas, rejected.Betas) {
				t.Fatalf("resend betas = %v, want %v", resend.Betas, rejected.Betas)
			}
			requireThreadFallbackDiagnostic(t, result, testCase.errorCode)

			messages = append(messages, threadTestUser("third"))
			h.say(t, messages, threadTurn{})
			requireThreadJSON(t, h.request(t, 3).thread(), `{"type":"continue","previous_message_id":"msg_3"}`)
		})
	}
}

// Upstream test: "starts a new thread when a retried continue finds the thread
// in flight".
func TestAnthropicMessageThreadsNewThreadWhenRetriedContinueIsInFlight(t *testing.T) {
	h := newThreadTestHarness(t)
	messages := []Message{threadTestUser("first")}
	messages, _ = h.say(t, messages, threadTurn{})
	messages = append(messages, threadTestUser("second"))
	h.push(func(threadTestCaptured) threadTestReply {
		return threadTestErrorReply(http.StatusServiceUnavailable, "overloaded", "", map[string]string{"retry-after-ms": "0"})
	})
	h.push(func(threadTestCaptured) threadTestReply {
		return threadTestErrorReply(http.StatusBadRequest, "in flight", "thread_continue_in_flight", nil)
	})
	_, result := h.say(t, messages, threadTurn{Options: func(options *AnthropicOptions) {
		options.MaxRetries = new(1)
	}})
	if result.StopReason != StopStop {
		t.Fatalf("stop reason = %q", result.StopReason)
	}
	if got, want := threadTypes(h.snapshot()), []string{"create", "continue", "continue", "create"}; !slices.Equal(got, want) {
		t.Fatalf("thread types = %v, want %v", got, want)
	}
}
