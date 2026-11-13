package ai

import (
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

const threadTestGrammarError = "The compiled grammar is too large, which would cause performance issues. " +
	"Simplify your tool schemas or reduce the number of strict tools."

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
	for _, beta := range strings.Split(value, ",") {
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
	status := reply.Status
	if status == 0 {
		status = http.StatusOK
	}
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
	return &AnthropicOptions{StreamOptions: StreamOptions{
		APIKey: "test-key", SessionID: h.sessionID, MaxRetries: &zeroRetries,
	}}
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

// Upstream test: "creates a new thread when the request has no messages beyond
// the stored prefix".
func TestAnthropicMessageThreadsStartNewThreadWithoutNewMessages(t *testing.T) {
	h := newThreadTestHarness(t)
	messages := []Message{threadTestUser("first")}
	messages, _ = h.say(t, messages, threadTurn{})
	h.turn(t, messages, threadTurn{})
	requireThreadType(t, h.request(t, 1), "create")
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

// Upstream test: "turns threads off for the session after %s on continue".
func TestAnthropicMessageThreadsTurnOffWhenUnsupported(t *testing.T) {
	cases := []struct {
		name      string
		errorCode string
		message   string
		reason    string
	}{
		{"thread_unsupported_request", "thread_unsupported_request", "thread_unsupported_request", "thread_unsupported_request"},
		{"a 400 without error code about the thread field", "", "thread: Extra inputs are not permitted", "400"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			h := newThreadTestHarness(t)
			messages := []Message{threadTestUser("first")}
			messages, _ = h.say(t, messages, threadTurn{})
			messages = append(messages, threadTestUser("second"))
			h.push(func(threadTestCaptured) threadTestReply {
				return threadTestErrorReply(http.StatusBadRequest, testCase.message, testCase.errorCode, nil)
			})
			messages, result := h.say(t, messages, threadTurn{})
			if result.StopReason != StopStop {
				t.Fatalf("stop reason = %q", result.StopReason)
			}
			requireThreadFallbackDiagnostic(t, result, testCase.reason)

			messages = append(messages, threadTestUser("third"))
			h.say(t, messages, threadTurn{})
			later := h.request(t, 3)
			requireThreadType(t, later, "")
			// The resend was accepted with the threads beta, so the beta list
			// stays the same.
			if !slices.Equal(later.Betas, h.request(t, 0).Betas) {
				t.Fatalf("later betas = %v, want %v", later.Betas, h.request(t, 0).Betas)
			}
		})
	}
}

// Upstream test: "shows the error when the resend fails too and keeps threads on".
func TestAnthropicMessageThreadsKeepThreadsOnWhenResendFails(t *testing.T) {
	h := newThreadTestHarness(t)
	messages := []Message{threadTestUser("first")}
	messages, _ = h.say(t, messages, threadTurn{})
	messages = append(messages, threadTestUser("second"))
	h.push(func(threadTestCaptured) threadTestReply {
		return threadTestErrorReply(http.StatusBadRequest, "thread: Extra inputs are not permitted", "", nil)
	})
	h.push(func(threadTestCaptured) threadTestReply {
		return threadTestErrorReply(http.StatusBadRequest, "Real problem", "", nil)
	})
	failed := h.turn(t, messages, threadTurn{})
	if failed.StopReason != StopError {
		t.Fatalf("stop reason = %q", failed.StopReason)
	}
	if failed.ErrorMessage == nil || !strings.Contains(*failed.ErrorMessage, "Real problem") {
		t.Fatalf("error message = %v", failed.ErrorMessage)
	}
	if h.count() != 3 {
		t.Fatalf("requests = %d", h.count())
	}

	h.turn(t, messages, threadTurn{})
	requireThreadType(t, h.request(t, 3), "create")
}

// Upstream test: "passes a %s that is not about the thread on without resending".
func TestAnthropicMessageThreadsPassThroughOtherErrors(t *testing.T) {
	cases := []struct {
		status  int
		message string
	}{
		{http.StatusBadRequest, "prompt is too long: 250000 tokens > 200000 maximum"},
		{http.StatusNotFound, "model: claude-test"},
	}
	for _, testCase := range cases {
		t.Run(fmt.Sprintf("%d", testCase.status), func(t *testing.T) {
			h := newThreadTestHarness(t)
			messages := []Message{threadTestUser("first")}
			messages, _ = h.say(t, messages, threadTurn{})
			messages = append(messages, threadTestUser("second"))
			h.push(func(threadTestCaptured) threadTestReply {
				return threadTestErrorReply(testCase.status, testCase.message, "", nil)
			})
			failed := h.turn(t, messages, threadTurn{})
			if failed.StopReason != StopError {
				t.Fatalf("stop reason = %q", failed.StopReason)
			}
			if failed.ErrorMessage == nil || !strings.Contains(*failed.ErrorMessage, testCase.message) {
				t.Fatalf("error message = %v", failed.ErrorMessage)
			}
			if h.count() != 2 {
				t.Fatalf("requests = %d", h.count())
			}
			if len(failed.Diagnostics) != 0 {
				t.Fatalf("diagnostics = %+v", failed.Diagnostics)
			}

			// Threads stay on: after a fix such as compaction, the next request
			// starts a new thread.
			h.turn(t, messages, threadTurn{})
			requireThreadType(t, h.request(t, 2), "create")
		})
	}
}

// Upstream test: "leaves strict tool rejections to the strict tool fallback".
func TestAnthropicMessageThreadsLeaveStrictToolRejectionsAlone(t *testing.T) {
	model := threadTestModel()
	model.Compat = &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{SupportsStrictTools: new(true)}}
	strictTool := threadTestTool("lookup")
	strictTool.ConstrainedSampling = ConstrainedSamplingValue{
		Set: true, Config: &ConstrainedSamplingConfig{Type: "json_schema", Strict: "prefer"},
	}
	h := newThreadTestHarness(t)
	h.push(func(threadTestCaptured) threadTestReply {
		return threadTestErrorReply(http.StatusBadRequest, threadTestGrammarError, "", nil)
	})
	result := h.turn(t, []Message{threadTestUser("first")}, threadTurn{Model: model, Tools: []Tool{strictTool}})
	if result.StopReason != StopStop {
		t.Fatalf("stop reason = %q", result.StopReason)
	}
	if h.count() != 2 {
		t.Fatalf("requests = %d", h.count())
	}
	if got, want := threadTypes(h.snapshot()), []string{"create", "create"}; !slices.Equal(got, want) {
		t.Fatalf("thread types = %v, want %v", got, want)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Type != "anthropic_strict_tools_fallback" {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
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

// Upstream test: "fails once, then turns threads off, with an explicit
// anthropic-beta header %s".
func TestAnthropicMessageThreadsExplicitBetaHeader(t *testing.T) {
	cases := []struct {
		name   string
		header *string
	}{
		{"without the threads beta", new("fine-grained-tool-streaming-2025-05-14")},
		{"set to null", nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			h := newThreadTestHarness(t)
			respond := func(request threadTestCaptured) threadTestReply {
				if request.thread() != nil && !slices.Contains(request.Betas, threadTestThreadsBeta) {
					return threadTestErrorReply(http.StatusBadRequest, "thread: Extra inputs are not permitted", "", nil)
				}
				return threadTestSSE(request, threadTestReplySpec{ID: request.ID})
			}
			h.push(respond)
			h.push(respond)
			h.push(respond)
			headers := ProviderHeaders{"anthropic-beta": testCase.header}
			messages := []Message{threadTestUser("first")}
			messages, result := h.say(t, messages, threadTurn{Options: func(options *AnthropicOptions) {
				options.Headers = headers
			}})
			if result.StopReason != StopStop {
				t.Fatalf("stop reason = %q", result.StopReason)
			}
			messages = append(messages, threadTestUser("second"))
			h.say(t, messages, threadTurn{Options: func(options *AnthropicOptions) {
				options.Headers = headers
			}})
			if got, want := threadTypes(h.snapshot()), []string{"create", "", ""}; !slices.Equal(got, want) {
				t.Fatalf("thread types = %v, want %v", got, want)
			}
		})
	}
}

// Upstream test: "does not record %s" (an aborted reply, a stream error, a
// stream that ends early, a refusal, a fallback model reply).
func TestAnthropicMessageThreadsDoNotRecordFailedReplies(t *testing.T) {
	cases := []struct {
		name string
		end  string
		spec threadTestReplySpec
	}{
		{"a stream error", "error_event", threadTestReplySpec{ID: "msg_bad"}},
		{"a stream that ends early", "no_message_stop", threadTestReplySpec{ID: "msg_bad"}},
		{"a refusal", "", threadTestReplySpec{ID: "msg_bad", StopReason: "refusal"}},
		{"a fallback model reply", "", threadTestReplySpec{ID: "msg_bad", Model: "claude-fallback"}},
	}
	for _, testCase := range cases {
		t.Run("does not record "+testCase.name, func(t *testing.T) {
			h := newThreadTestHarness(t)
			spec := testCase.spec
			spec.End = testCase.end
			h.push(func(request threadTestCaptured) threadTestReply {
				return threadTestSSE(request, spec)
			})
			messages := []Message{threadTestUser("first")}
			first := h.turn(t, messages, threadTurn{})
			messages = append(messages, first, threadTestUser("second"))
			h.turn(t, messages, threadTurn{})
			requireThreadType(t, h.request(t, 1), "create")
		})
	}
	t.Run("does not record an aborted reply", func(t *testing.T) {
		h := newThreadTestHarness(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		messages := []Message{threadTestUser("first")}
		first := h.turn(t, messages, threadTurn{Options: func(options *AnthropicOptions) {
			options.Ctx = ctx
			options.OnProviderStreamEvent = func(data json.RawMessage, model *Model) {
				var event struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(data, &event) == nil && event.Type == "message_start" {
					cancel()
				}
			}
		}})
		messages = append(messages, first, threadTestUser("second"))
		h.turn(t, messages, threadTurn{})
		requireThreadType(t, h.request(t, 1), "create")
	})
}

// Upstream test: "does not use threads with cacheRetention none".
func TestAnthropicMessageThreadsOffWithCacheRetentionNone(t *testing.T) {
	h := newThreadTestHarness(t)
	messages := []Message{threadTestUser("first")}
	h.say(t, messages, threadTurn{Options: func(options *AnthropicOptions) {
		options.CacheRetention = CacheRetentionNone
	}})
	request := h.request(t, 0)
	requireThreadType(t, request, "")
	if slices.Contains(request.Betas, threadTestThreadsBeta) {
		t.Fatalf("betas = %v", request.Betas)
	}
}

// Upstream test: "matches Claude Code tool names across turns with OAuth".
func TestAnthropicMessageThreadsMatchClaudeCodeToolNamesAcrossTurns(t *testing.T) {
	h := newThreadTestHarness(t)
	h.push(func(request threadTestCaptured) threadTestReply {
		return threadTestSSE(request, threadTestReplySpec{
			ID: "msg_oauth", ToolCalls: []threadTestToolCall{{ID: "toolu_1", Name: "Read", Input: map[string]any{"value": "x"}}},
		})
	})
	messages := []Message{threadTestUser("read it")}
	messages, first := h.say(t, messages, threadTurn{Tools: []Tool{threadTestTool("read")}, Options: func(options *AnthropicOptions) {
		options.APIKey = "sk-ant-oat-test"
	}})
	found := false
	for _, block := range first.Content {
		if call, ok := block.(ToolCall); ok && call.Name == "read" {
			found = true
		}
	}
	if !found {
		t.Fatalf("content = %+v", first.Content)
	}
	messages = append(messages, threadTestToolResult("toolu_1", "read"))
	h.say(t, messages, threadTurn{Tools: []Tool{threadTestTool("read")}, Options: func(options *AnthropicOptions) {
		options.APIKey = "sk-ant-oat-test"
	}})
	requireThreadJSON(t, h.request(t, 1).thread(), `{"type":"continue","previous_message_id":"msg_oauth"}`)
}

// Upstream test: "sends cache warming replays without a thread and keeps the
// conversation's thread".
func TestAnthropicMessageThreadsSkipDisabledContinuation(t *testing.T) {
	h := newThreadTestHarness(t)
	messages := []Message{threadTestUser("first")}
	messages, _ = h.say(t, messages, threadTurn{})
	h.turn(t, messages[:1], threadTurn{Options: func(options *AnthropicOptions) {
		options.MaxTokens = new(1)
		options.DisableContinuation = true
	}})
	messages = append(messages, threadTestUser("second"))
	h.say(t, messages, threadTurn{})

	real, warm, next := h.request(t, 0), h.request(t, 1), h.request(t, 2)
	requireThreadType(t, warm, "")
	if warm.Body["max_tokens"] != float64(1) {
		t.Fatalf("max_tokens = %v", warm.Body["max_tokens"])
	}
	requireThreadJSON(t, warm.messages(), string(mustMarshalJSON(real.messages())))
	if !slices.Equal(warm.Betas, real.Betas) {
		t.Fatalf("warm betas = %v, want %v", warm.Betas, real.Betas)
	}
	requireThreadJSON(t, next.thread(), `{"type":"continue","previous_message_id":"msg_1"}`)
}

// Reference: pi v1.5.2 (b4afdddf) api/anthropic-messages.ts getAnthropicCompat:
// message threads default on only for provider anthropic on api.anthropic.com,
// and a compat entry overrides the default either way.
func TestAnthropicMessageThreadsCompatDefault(t *testing.T) {
	cases := []struct {
		name     string
		provider ProviderId
		baseURL  string
		compat   *bool
		want     bool
	}{
		{"anthropic endpoint", "anthropic", "https://api.anthropic.com", nil, true},
		{"anthropic proxy", "anthropic", "https://proxy.example.com", nil, false},
		{"other provider on the anthropic endpoint", "openrouter", "https://api.anthropic.com", nil, false},
		{"compat enables a proxy", "anthropic", "https://proxy.example.com", new(true), true},
		{"compat disables the anthropic endpoint", "anthropic", "https://api.anthropic.com", new(false), false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			model := threadTestModel()
			model.Provider = testCase.provider
			model.BaseURL = testCase.baseURL
			if testCase.compat != nil {
				model.Compat = &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{SupportsMessageThreads: testCase.compat}}
			}
			if got := GetAnthropicCompat(model).SupportsMessageThreads; got != testCase.want {
				t.Fatalf("supportsMessageThreads = %v, want %v", got, testCase.want)
			}
			if got := slices.Contains(GetBetaFeatures(model, TranscriptContext{}, false, false, testCase.want, nil), MessageThreadsBeta); got != testCase.want {
				t.Fatalf("threads beta = %v, want %v", got, testCase.want)
			}
		})
	}
}

// A model on a non-Anthropic endpoint sends no thread and no threads beta,
// unless its models.json compat entry asks for threads.
func TestAnthropicMessageThreadsFollowCompatOnProxyEndpoints(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		compat *bool
		want   string
	}{
		{"default off", nil, ""},
		{"compat on", new(true), "create"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			h := newThreadTestHarness(t)
			model := threadTestModel()
			model.BaseURL = "https://proxy.example.com"
			if testCase.compat != nil {
				model.Compat = &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{SupportsMessageThreads: testCase.compat}}
			}
			messages := []Message{threadTestUser("first")}
			messages, _ = h.say(t, messages, threadTurn{Model: model})
			messages = append(messages, threadTestUser("second"))
			h.say(t, messages, threadTurn{Model: model})
			requireThreadType(t, h.request(t, 0), testCase.want)
			if testCase.want != "" {
				requireThreadType(t, h.request(t, 1), "continue")
			} else {
				requireThreadType(t, h.request(t, 1), "")
			}
			hasBeta := slices.Contains(h.request(t, 0).Betas, threadTestThreadsBeta)
			if hasBeta != (testCase.want != "") {
				t.Fatalf("betas = %v", h.request(t, 0).Betas)
			}
		})
	}
}

// The thread map is bounded: the least recently updated session is dropped.
func TestAnthropicMessageThreadsStateIsBounded(t *testing.T) {
	ResetAnthropicMessageThreads("")
	t.Cleanup(func() { ResetAnthropicMessageThreads("") })
	for index := 0; index < maxAnthropicThreadStateEntries+2; index++ {
		setAnthropicThreadState(fmt.Sprintf("thread-bound-%d", index), &anthropicThreadEntry{prefixCount: index})
	}
	anthropicThreadMu.Lock()
	size := len(anthropicThreadEntries)
	ordered := len(anthropicThreadOrder)
	anthropicThreadMu.Unlock()
	if size != maxAnthropicThreadStateEntries || ordered != maxAnthropicThreadStateEntries {
		t.Fatalf("entries = %d/%d, want %d", size, ordered, maxAnthropicThreadStateEntries)
	}
	if _, known := getAnthropicThreadState("thread-bound-0"); known {
		t.Fatal("oldest session kept")
	}
	if _, known := getAnthropicThreadState("thread-bound-1"); known {
		t.Fatal("second oldest session kept")
	}
	if _, known := getAnthropicThreadState("thread-bound-2"); !known {
		t.Fatal("newest session dropped")
	}
	ResetAnthropicMessageThreads("thread-bound-2")
	if _, known := getAnthropicThreadState("thread-bound-2"); known {
		t.Fatal("reset kept the session")
	}
}

// Reference: pi v1.5.2 (b4afdddf) api/anthropic-messages.ts hashThreadPrefix.
// Its replacer drops cache_control and sends string content as one text block;
// the port keeps that normalization. D459: the hash itself is canonical JSON
// with sorted keys, so member order does not change it.
func TestAnthropicMessageThreadsPrefixHashNormalization(t *testing.T) {
	model := threadTestModel()
	settings, err := decodeOrderedJSONFields(json.RawMessage(`{"model":"claude-test"}`))
	if err != nil {
		t.Fatal(err)
	}
	hash := func(raw ...string) string {
		t.Helper()
		messages := make([]json.RawMessage, 0, len(raw))
		for _, message := range raw {
			messages = append(messages, json.RawMessage(message))
		}
		return hashAnthropicThreadPrefix(model, settings, messages)
	}
	cached := hash(`{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}`)
	plain := hash(`{"role":"user","content":[{"type":"text","text":"hi"}]}`)
	if cached != plain {
		t.Fatalf("cache marker changed the hash: %q vs %q", cached, plain)
	}
	if asString := hash(`{"role":"user","content":"hi"}`); asString != plain {
		t.Fatalf("string content changed the hash: %q vs %q", asString, plain)
	}
	if reordered := hash(`{"content":"hi","role":"user"}`); reordered != plain {
		t.Fatalf("member order changed the hash: %q vs %q", reordered, plain)
	}
	if changed := hash(`{"role":"user","content":"bye"}`); changed == plain {
		t.Fatalf("different content hashed alike: %q", changed)
	}
	otherSettings, err := decodeOrderedJSONFields(json.RawMessage(`{"model":"claude-other"}`))
	if err != nil {
		t.Fatal(err)
	}
	if hashAnthropicThreadPrefix(model, otherSettings, []json.RawMessage{json.RawMessage(`{"role":"user","content":"hi"}`)}) == plain {
		t.Fatal("a changed setting hashed alike")
	}
}
