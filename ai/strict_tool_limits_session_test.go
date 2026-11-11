package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

// pi v1.5.2 (b4afdddf) api/strict-tool-limits.ts: once a session's strict tools
// are rejected for Claude's limits, every later request of that session sends
// preferred strict tools non-strict at once, so the rejection is not repeated.
// Rejecting it again would also miss the prompt cache when strictness returns.

func strictLimitTool() Tool {
	return Tool{
		Name: "lookup", Description: "Look up a value",
		Parameters:          json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`),
		ConstrainedSampling: ConstrainedSamplingValue{Set: true, Config: &ConstrainedSamplingConfig{Type: "json_schema", Strict: "prefer"}},
	}
}

// anthropicStrictLimitServer rejects the first request of each call with the
// documented message and answers with an empty stream afterwards, recording
// whether each request declared the preferred tool strict.
func anthropicStrictLimitServer(t *testing.T) (*httptest.Server, func() (attempts int, strictFlags []bool), func()) {
	t.Helper()
	var mu sync.Mutex
	var flags []bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var payload struct {
			Tools []struct {
				Strict *bool `json:"strict"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		strict := len(payload.Tools) > 0 && payload.Tools[0].Strict != nil && *payload.Tools[0].Strict
		flags = append(flags, strict)
		// Reject exactly the requests that declare the tool strict, as Claude does.
		if strict {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"The compiled grammar is too large"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"claude-opus-4-5\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	snapshot := func() (int, []bool) {
		mu.Lock()
		defer mu.Unlock()
		return len(flags), append([]bool(nil), flags...)
	}
	reset := func() {
		mu.Lock()
		defer mu.Unlock()
		flags = nil
	}
	return server, snapshot, reset
}

func TestStreamAnthropicSkipsPreferredStrictToolsForALatchedSession(t *testing.T) {
	server, snapshot, reset := anthropicStrictLimitServer(t)
	defer server.Close()
	model := testAnthropicModel()
	model.BaseURL = server.URL
	transcript := NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1}},
		Tools:    []Tool{strictLimitTool()},
	})
	run := func(sessionID string) int {
		t.Helper()
		message, err := StreamAnthropic(model, transcript, &AnthropicOptions{StreamOptions: StreamOptions{APIKey: "test-key", SessionID: sessionID}}).Result(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if message.StopReason != StopStop {
			text := ""
			if message.ErrorMessage != nil {
				text = *message.ErrorMessage
			}
			t.Fatalf("stop = %s (%s)", message.StopReason, text)
		}
		attempts, _ := snapshot()
		return attempts
	}

	// The first call is rejected and retried once without strict tools.
	if got := run("session-latched"); got != 2 {
		t.Fatalf("first call attempts = %d, want 2", got)
	}
	reset()
	// The same session starts without strict tools, so one request suffices.
	if got := run("session-latched"); got != 1 {
		t.Fatalf("second call of the same session attempts = %d, want 1", got)
	}
	if attempts, flags := snapshot(); attempts != 1 || flags[0] {
		t.Fatalf("second call sent strict=%v over %d requests", flags, attempts)
	}
	reset()
	// A session that never saw the rejection still tries strict tools first.
	if got := run("session-fresh"); got != 2 {
		t.Fatalf("fresh session attempts = %d, want 2", got)
	}
	if _, flags := snapshot(); len(flags) != 2 || !flags[0] {
		t.Fatalf("fresh session strict flags = %v", flags)
	}
}

func TestBedrockSkipsPreferredStrictToolsForALatchedSession(t *testing.T) {
	frames := [][]byte{
		bedrockEventFrame("messageStart", map[string]any{"role": "assistant"}),
		bedrockEventFrame("contentBlockDelta", map[string]any{"contentBlockIndex": 0, "delta": map[string]any{"text": "ok"}}),
		bedrockEventFrame("contentBlockStop", map[string]any{"contentBlockIndex": 0}),
		bedrockEventFrame("messageStop", map[string]any{"stopReason": "end_turn"}),
	}
	var mu sync.Mutex
	var strictFlags []bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		raw, _ := io.ReadAll(request.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		strict := false
		if config, ok := body["toolConfig"].(map[string]any); ok {
			if tools, ok := config["tools"].([]any); ok && len(tools) > 0 {
				if spec, ok := tools[0].(map[string]any)["toolSpec"].(map[string]any); ok {
					strict = spec["strict"] == true
				}
			}
		}
		strictFlags = append(strictFlags, strict)
		if strict {
			writer.Header().Set("x-amzn-errortype", "ValidationException")
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"message":"Too many strict tools (20)"}`))
			return
		}
		writer.Header().Set("x-amzn-requestid", "req")
		writer.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		writer.WriteHeader(http.StatusOK)
		for _, frame := range frames {
			_, _ = writer.Write(frame)
		}
	}))
	defer server.Close()

	model := bedrockTestModel(server.URL)
	strictSupport := true
	model.Compat = &ModelCompat{Bedrock: &BedrockCompat{SupportsStrictMode: &strictSupport}}
	transcript := TranscriptContext{Messages: []Message{
		&SystemMessage{Content: StringOrBlocks{Text: "be helpful"}, ToolsAdded: []Tool{strictLimitTool()}},
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}
	run := func(sessionID string) int {
		t.Helper()
		message, err := StreamBedrockConverse(model, transcript, &BedrockOptions{
			BearerToken:   "bearer-token",
			StreamOptions: StreamOptions{APIKey: "bearer-token", SessionID: sessionID},
		}).Result(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if message.StopReason != StopStop {
			text := ""
			if message.ErrorMessage != nil {
				text = *message.ErrorMessage
			}
			t.Fatalf("stop = %s (%s)", message.StopReason, text)
		}
		mu.Lock()
		defer mu.Unlock()
		return len(strictFlags)
	}
	snapshot := func() []bool {
		mu.Lock()
		defer mu.Unlock()
		return append([]bool(nil), strictFlags...)
	}
	reset := func() {
		mu.Lock()
		defer mu.Unlock()
		strictFlags = nil
	}

	if got := run("bedrock-latched"); got != 2 {
		t.Fatalf("first call attempts = %d, want 2", got)
	}
	reset()
	if got := run("bedrock-latched"); got != 1 {
		t.Fatalf("second call of the same session attempts = %d, want 1", got)
	}
	if flags := snapshot(); len(flags) != 1 || flags[0] {
		t.Fatalf("second call strict flags = %v", flags)
	}
}

// The latch is per session, bounded, and cleared when a session ends
// (pi v1.5.2 api/strict-tool-limits.ts).
func TestStrictToolLimitsLatchIsBoundedAndClears(t *testing.T) {
	ResetStrictToolLimits("")
	t.Cleanup(func() { ResetStrictToolLimits("") })
	if HasExceededStrictToolLimits("") {
		t.Fatal("the empty session id must never latch")
	}
	MarkStrictToolLimitsExceeded("")
	if HasExceededStrictToolLimits("") {
		t.Fatal("marking the empty session id must do nothing")
	}

	MarkStrictToolLimitsExceeded("s1")
	if !HasExceededStrictToolLimits("s1") || HasExceededStrictToolLimits("s2") {
		t.Fatal("the latch is not per session")
	}
	ResetStrictToolLimits("s1")
	if HasExceededStrictToolLimits("s1") {
		t.Fatal("ResetStrictToolLimits kept the session")
	}

	// The cap drops the oldest session, so a library that never cleans up is
	// bounded rather than growing without limit.
	for index := 0; index <= maxSessionsOverStrictToolLimits; index++ {
		MarkStrictToolLimitsExceeded("cap-" + strconv.Itoa(index))
	}
	if HasExceededStrictToolLimits("cap-0") {
		t.Fatal("the oldest session survived the cap")
	}
	if !HasExceededStrictToolLimits("cap-" + strconv.Itoa(maxSessionsOverStrictToolLimits)) {
		t.Fatal("the newest session was dropped")
	}
	ResetStrictToolLimits("")
	if HasExceededStrictToolLimits("cap-" + strconv.Itoa(maxSessionsOverStrictToolLimits)) {
		t.Fatal("an empty reset kept a session")
	}
}
