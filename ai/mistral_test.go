package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// pi v1.4.0 (4a080252), api/mistral-conversations.ts consumeChatStream:
// every tool delta ends the active text/thinking block and clears currentBlock,
// even when it updates an earlier tool call rather than appending a new one.
func TestMistralContinuedToolCallClosesTextBlock(t *testing.T) {
	chunks := []string{
		`{"choices":[{"delta":{"tool_calls":[{"id":"abc123XYZ","index":0,"function":{"name":"lookup","arguments":"{\"pa"}}]}}]}`,
		`{"choices":[{"delta":{"content":"middle"}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"lookup","arguments":"th\":\"a\"}"}}]}}]}`,
		`{"choices":[{"delta":{"content":"after"},"finish_reason":"tool_calls"}]}`,
	}
	var sse strings.Builder
	for _, chunk := range chunks {
		sse.WriteString("data: ")
		sse.WriteString(chunk)
		sse.WriteString("\n\n")
	}
	sse.WriteString("data: [DONE]\n\n")
	server := sseServer(t, []string{sse.String()}, nil)
	defer server.Close()
	stream := StreamMistralConversations(mistralModel(server.URL), helloUserContext(), &MistralOptions{
		APIKey: "test-key",
	})
	var types []string
	for {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		types = append(types, event.Type)
	}
	want := []string{"start", "toolcall_start", "toolcall_delta", "text_start", "text_delta", "text_end", "toolcall_delta", "text_start", "text_delta", "text_end", "toolcall_end", "done"}
	if !reflect.DeepEqual(types, want) {
		t.Fatalf("events = %v, want %v", types, want)
	}
	message, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 3 {
		t.Fatalf("content = %+v, want tool call and two separate text blocks", message.Content)
	}
	for i, want := range []string{"middle", "after"} {
		text, ok := message.Content[i+1].(TextContent)
		if !ok || text.Text != want {
			t.Errorf("content[%d] = %+v, want text %q", i+1, message.Content[i+1], want)
		}
	}
}

// pi v1.4.0 (4a080252), api/mistral-conversations.ts streamSimple selects
// reasoning_effort from the model map, not from hardcoded model ids.
// Regenerate:
//
//	node --experimental-strip-types ai/testdata/mistral-simple/generate.mjs ./pi > ai/testdata/mistral-simple/golden.json
func TestMistralSimpleReasoningMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/mistral-simple/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	type reasoningPayload struct {
		ReasoningEffort string `json:"reasoningEffort,omitempty"`
		PromptMode      string `json:"promptMode,omitempty"`
	}
	var fixtures []struct {
		Name      string           `json:"name"`
		Model     Model            `json:"model"`
		Reasoning ThinkingLevel    `json:"reasoning"`
		Expected  reasoningPayload `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty Mistral simple corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			server := sseServer(t, []string{"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"}, nil)
			defer server.Close()
			fixture.Model.BaseURL = server.URL
			var got reasoningPayload
			stream := StreamMistralConversationsSimple(&fixture.Model, helloUserContext(), &SimpleStreamOptions{
				Reasoning: fixture.Reasoning,
				APIKey:    "test-key", OnPayload: func(payload json.RawMessage, model *Model) json.RawMessage {
					if err := json.Unmarshal(payload, &got); err != nil {
						t.Error(err)
					}
					return nil
				},
			})
			message, err := stream.Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if message.StopReason != StopStop {
				t.Fatalf("stream failed: %+v", message)
			}
			if got != fixture.Expected {
				t.Fatalf("reasoning payload = %+v, want %+v", got, fixture.Expected)
			}
		})
	}
}

// pi v1.4.0 (4a080252), api/mistral-conversations.ts. Regenerate:
//
//	node --experimental-strip-types ai/testdata/mistral-stream/generate.mjs ./pi > ai/testdata/mistral-stream/golden.json
//
// Compare event scalars and terminal messages, not the live partial helper or clocks.
func TestMistralStreamEventsMatchUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/mistral-stream/golden.json")
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
		t.Fatal("empty Mistral stream corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(fixture.SSE))
			}))
			defer server.Close()
			model := mistralModel(server.URL)
			model.Cost = ModelCost{Input: 2, Output: 6, CacheRead: 0.5}
			stream := StreamMistralConversations(model, helloUserContext(), &MistralOptions{APIKey: "test-key"})
			checkProjectedEvents(t, stream, fixture.Events)
		})
	}
}

// pi v1.4.0 mistral-conversations.ts uses options?.timeoutMs ?? 60_000;
// explicit zero must immediately abort the response-header wait.
func TestMistralAbortSignalTimeoutRejectsOutOfRangeDelays(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	for _, timeoutMS := range []int{-1, 4_294_967_296} {
		t.Run(strconv.Itoa(timeoutMS), func(t *testing.T) {
			requests.Store(0)
			response, err := requestMistralStream(context.Background(), &Model{BaseURL: server.URL}, []byte(`{}`), &MistralOptions{
				APIKey: "test", TimeoutMs: &timeoutMS,
			})
			if response != nil {
				response.Body.Close()
			}
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "out of range") {
				t.Fatalf("request error = %v, want timeout range error", err)
			}
			if got := requests.Load(); got != 0 {
				t.Fatalf("fetch requests = %d, want validation before fetch", got)
			}
		})
	}
}

func TestMistralExplicitZeroTimeoutAbortsHeaderWait(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		select {
		case <-request.Context().Done():
		case <-time.After(20 * time.Millisecond):
		}
	}))
	defer server.Close()

	zero := 0
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	model := &Model{BaseURL: server.URL}
	_, err := requestMistralStream(ctx, model, []byte(`{}`), &MistralOptions{
		APIKey: "test", TimeoutMs: &zero,
	})
	if err == nil || !strings.Contains(err.Error(), "Mistral response headers timed out after 0ms") {
		t.Fatalf("request error = %v, want immediate 0ms header timeout", err)
	}
}

// pi v1.4.0 (4a080252), api/mistral-conversations.ts consumeChatStream:
// string arguments are used verbatim; absent/null non-string arguments use
// JSON.stringify(arguments || {}), producing an object delta.
func TestMistralDefaultsAbsentToolArgumentsToObjectDelta(t *testing.T) {
	for _, fixture := range []struct {
		name, arguments, want string
	}{
		{"absent", "", "{}"},
		{"null", `,"arguments":null`, "{}"},
		{"empty-string", `,"arguments":""`, ""},
		{"object", `,"arguments":{"path":"a"}`, `{"path":"a"}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			body := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call0\",\"index\":0,\"function\":{\"name\":\"lookup\"" + fixture.arguments + "}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"
			server := sseServer(t, []string{body}, nil)
			defer server.Close()
			stream := StreamMistralConversations(mistralModel(server.URL), helloUserContext(), &MistralOptions{
				APIKey: "test-key",
			})
			var deltas []string
			for {
				event, ok := stream.Next(context.Background())
				if !ok {
					break
				}
				if event.Type == EventError {
					t.Fatalf("stream failed: %+v", event.Error)
				}
				if event.Type == EventToolcallDelta {
					deltas = append(deltas, event.Delta)
				}
			}
			if len(deltas) != 1 || deltas[0] != fixture.want {
				t.Fatalf("deltas = %#v, want [%q]", deltas, fixture.want)
			}
		})
	}
}

// pi v1.4.0 (4a080252), api/mistral-conversations.ts consumeChatStream
// finalizes toolBlocksByKey.values() in first-seen order, not numeric index or
// Go map order. Nine calls exercise a map spanning more than one bucket.
func TestMistralToolCallsEndInFirstSeenOrder(t *testing.T) {
	order := []int{8, 3, 5, 0, 7, 2, 6, 1, 4}
	var body strings.Builder
	for _, index := range order {
		fmt.Fprintf(&body, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call%d\",\"index\":%d,\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]}}]}\n\n", index, index)
	}
	body.WriteString("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
	server := sseServer(t, []string{body.String()}, nil)
	defer server.Close()
	stream := StreamMistralConversations(mistralModel(server.URL), helloUserContext(), &MistralOptions{
		APIKey: "test-key",
	})
	var got []string
	for {
		event, ok := stream.Next(context.Background())
		if !ok {
			break
		}
		if event.Type == EventError {
			t.Fatalf("stream failed: %+v", event.Error)
		}
		if event.Type == EventToolcallEnd {
			got = append(got, event.ToolCall.ID)
		}
	}
	want := []string{"call8", "call3", "call5", "call0", "call7", "call2", "call6", "call1", "call4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("toolcall_end order = %v, want %v", got, want)
	}
}

// Reference: pi v1.5.0 (f9a26365), api/mistral-conversations.ts
// consumeChatStream preserves finite provider usage as JavaScript Numbers.
// D366 saturates counts outside the port's int64 usage representation.
func TestMistralStreamSaturatesFiniteUsageBeyondInt64(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: " + `{"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":1e308,"completion_tokens":1e308,"total_tokens":1e308}}` + "\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	stream := StreamMistralConversations(mistralModel(server.URL), helloUserContext(), &MistralOptions{APIKey: "test-key"})
	message, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message.StopReason != StopStop {
		t.Fatalf("stop reason = %s, error = %v", message.StopReason, message.ErrorMessage)
	}
	if message.Usage.Input != math.MaxInt64 || message.Usage.Output != math.MaxInt64 || message.Usage.TotalTokens != math.MaxInt64 {
		t.Fatalf("usage = %+v, want positive saturated input, output and total", message.Usage)
	}
}

// Reference: pi v1.5.0 (f9a26365) packages/ai/src/api/mistral-conversations.ts
// buildChatPayload builds `messages: toChatMessages(...)` unconditionally
// (array, never null), and toChatMessages returns an empty array when no
// message survives the conversion (an assistant whose only content is an
// empty-text block is one such input). A423: pier marshalled the nil slice,
// so the wire writes `"messages": null` where the reference writes `[]`.
func TestMistralMessagesIsAnArrayEvenWhenEmpty(t *testing.T) {
	messages := []Message{
		&AssistantMessage{Content: ContentList{TextContent{Text: "  "}}},
	}
	got := toMistralChatMessages(messages, false)
	if len(got) != 0 {
		t.Fatalf("messages = %#v, want none", got)
	}
	payload := &mistralChatPayload{
		Model: "codestral-2508", Stream: true, Messages: got,
	}
	encoded, err := mistralWirePayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]any
	if err := json.Unmarshal(encoded, &probe); err != nil {
		t.Fatal(err)
	}
	if _, ok := probe["messages"]; !ok {
		t.Fatalf("messages key missing: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"messages":[]`) {
		t.Fatalf("wire = %s; want an empty messages array", encoded)
	}
	if !strings.Contains(string(encoded), `"messages":null`) {
		t.Logf("the empty form is pinned: %s", encoded)
	}
}

// Reference: pi v1.4.0 (4a080252), api/mistral-conversations.ts stream,
// buildChatPayload and toMistralWirePayload. Captures the public HTTP boundary.
// Regenerate: node --experimental-strip-types testdata/mistral-wire/generate.mjs ../pi > testdata/mistral-wire/golden.json
func TestMistralRequestsMatchUpstreamWire(t *testing.T) {
	data, err := os.ReadFile("testdata/mistral-wire/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name        string            `json:"name"`
		ForcedTool  string            `json:"forcedTool"`
		Messages    []json.RawMessage `json:"messages"`
		Options     MistralOptions    `json:"options"`
		Replacement json.RawMessage   `json:"replacement"`
		Payload     string            `json:"payload"`
		Path        string            `json:"path"`
		Body        string            `json:"body"`
		Error       string            `json:"error"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			bodies := make(chan string, 1)
			paths := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, _ := io.ReadAll(request.Body)
				bodies <- string(body)
				paths <- request.URL.Path
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(writer, "data: {\"id\":\"r\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			model := mistralModel(server.URL)
			model.Reasoning = true
			model.Input = []string{"text", "image"}
			var messages []Message
			for _, raw := range fixture.Messages {
				message, err := UnmarshalMessage(raw)
				if err != nil {
					t.Fatal(err)
				}
				messages = append(messages, message)
			}
			payloads := make(chan string, 1)
			options := fixture.Options
			if fixture.ForcedTool != "" {
				options.ToolChoice = "function"
				options.ToolChoiceFunction = fixture.ForcedTool
			}
			options.APIKey = "token"
			options.Env = ProviderEnv{}
			options.OnPayload = func(payload json.RawMessage, _ *Model) json.RawMessage {
				payloads <- string(payload)
				return fixture.Replacement
			}
			result, err := StreamMistralConversations(model, TranscriptContext{Messages: messages}, &options).Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if fixture.Error != "" {
				if result.StopReason != StopError || result.ErrorMessage == nil || *result.ErrorMessage != fixture.Error {
					t.Fatalf("result = %+v, want error %q", result, fixture.Error)
				}
				select {
				case body := <-bodies:
					t.Fatalf("invalid strict schema sent an HTTP request: %s", body)
				case payload := <-payloads:
					t.Fatalf("invalid strict schema reached the hook: %s", payload)
				default:
				}
				return
			}
			if result.StopReason != StopStop {
				t.Fatalf("stopReason = %s, error = %v", result.StopReason, result.ErrorMessage)
			}
			if got := <-payloads; got != fixture.Payload {
				t.Errorf("payload = %s\nwant      %s", got, fixture.Payload)
			}
			if got := <-paths; got != fixture.Path {
				t.Errorf("path = %s, want %s", got, fixture.Path)
			}
			if got := <-bodies; got != fixture.Body {
				t.Fatalf("body = %s\nwant   %s", got, fixture.Body)
			}
		})
	}
}

// Mistral's strict tools are opt-in per model: `toFunctionTools` passes
// `model.compat.supportsStrictMode ?? false`, because Mistral Large 4 can go
// silent for minutes before a strict tool call (mistral-conversations.ts,
// v1.6.0, #12148). Without the compat flag a "prefer" tool is sent non-strict.
func TestMistralStrictToolsAreOptIn(t *testing.T) {
	strictPreference := ConstrainedSamplingValue{Set: true, Config: &ConstrainedSamplingConfig{Type: "json_schema", Strict: "prefer"}}
	declared := SystemMessage{Content: StringOrBlocks{Text: "system"}, Timestamp: 1}
	declared.ToolsAdded = []Tool{{
		Name: "lookup", Description: "Look up",
		Parameters:          json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`),
		ConstrainedSampling: strictPreference,
	}}
	build := func(compat *MistralConversationsCompat) mistralFunctionTool {
		model := &Model{ID: "mistral-large-latest", API: APIMistralConversations, Provider: "mistral", Compat: &ModelCompat{MistralConversations: compat}}
		payload, err := buildMistralChatPayload(model, TranscriptContext{Messages: []Message{
			&declared,
			&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 2},
		}}, nil, &MistralOptions{})
		if err != nil {
			t.Fatalf("build payload: %v", err)
		}
		if len(payload.Tools) == 0 {
			t.Fatal("no tools in the payload")
		}
		return payload.Tools[0]
	}
	if got := build(nil); got.Function.Strict {
		t.Fatalf("strict = true without the compat flag, want the tool sent non-strict")
	}
	on := true
	if got := build(&MistralConversationsCompat{SupportsStrictMode: &on}); !got.Function.Strict {
		t.Fatalf("strict = false with supportsStrictMode, want a strict tool")
	}
}
