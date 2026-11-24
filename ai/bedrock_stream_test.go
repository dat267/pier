package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Tests for api/bedrock-converse-stream.ts (message conversion, request
// construction, and the streaming event fold).

func bedrockTestModel(baseURL string) *Model {
	return &Model{
		ID: "anthropic.claude-sonnet-4-5-20250929-v1:0", Name: "Claude Sonnet 4.5",
		API: APIBedrockConverse, Provider: "amazon-bedrock", BaseURL: baseURL,
		Reasoning: true, Input: []string{"text", "image"}, ContextWindow: 200000, MaxTokens: 8192,
		Cost: ModelCost{ModelCostRates: ModelCostRates{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75}},
	}
}

// bedrockTestServer streams encoded event frames.
func bedrockTestServer(t *testing.T, frames [][]byte, capture *http.Request, body *map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if capture != nil {
			*capture = *request
		}
		if body != nil {
			raw, _ := io.ReadAll(request.Body)
			_ = json.Unmarshal(raw, body)
		}
		writer.Header().Set("x-amzn-requestid", "req-1")
		writer.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		writer.WriteHeader(http.StatusOK)
		for _, frame := range frames {
			_, _ = writer.Write(frame)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func bedrockEventFrame(eventType string, payload any) []byte {
	encoded, _ := json.Marshal(payload)
	return buildAWSEventStreamMessageFor(nil, map[string]any{
		":message-type": "event",
		":event-type":   eventType,
		":content-type": "application/json",
	}, encoded)
}

// Claude on Bedrock rejects error tool results that contain anything but text,
// so their images follow all tool results of the turn (#10794).
func TestBedrockErrorToolResultImagesFollowToolResults(t *testing.T) {
	claude := &Model{ID: "anthropic.claude-sonnet-4-5-v1:0", Name: "Claude", Input: []string{"text", "image"}}
	messages, err := ConvertBedrockMessages(TranscriptContext{Messages: []Message{
		&ToolResultMessage{
			ToolCallID: "call-1", ToolName: "read", IsError: true,
			Content: UserContentList{
				TextContent{Text: "failed"},
				ImageContent{Data: "AAAA", MimeType: "image/png"},
			},
		},
	}}, claude, CacheRetentionNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	user, _ := messages[0].(map[string]any)
	blocks, _ := user["content"].([]any)
	// The text-only tool result, then the caption and the image.
	if len(blocks) != 3 {
		t.Fatalf("blocks = %#v", blocks)
	}
	result, _ := blocks[0].(map[string]any)["toolResult"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("tool result content = %#v", content)
	}
	if note, _ := content[1].(map[string]any); note["text"] != "(image attached after the tool results)" {
		t.Fatalf("tool result note = %#v", content[1])
	}
	if caption, _ := blocks[1].(map[string]any); caption["text"] != "Images from the error result of tool call call-1 (read):" {
		t.Fatalf("caption = %#v", blocks[1])
	}
	if image, _ := blocks[2].(map[string]any); image["image"] == nil {
		t.Fatalf("image = %#v", blocks[2])
	}
}

func TestBedrockStreamFold(t *testing.T) {
	frames := [][]byte{
		bedrockEventFrame("messageStart", map[string]any{"role": "assistant"}),
		bedrockEventFrame("contentBlockDelta", map[string]any{"contentBlockIndex": 0, "delta": map[string]any{"text": "Hel"}}),
		bedrockEventFrame("contentBlockDelta", map[string]any{"contentBlockIndex": 0, "delta": map[string]any{"text": "lo"}}),
		bedrockEventFrame("contentBlockStop", map[string]any{"contentBlockIndex": 0}),
		bedrockEventFrame("contentBlockStart", map[string]any{"contentBlockIndex": 1, "start": map[string]any{"toolUse": map[string]any{"toolUseId": "call-1", "name": "read"}}}),
		bedrockEventFrame("contentBlockDelta", map[string]any{"contentBlockIndex": 1, "delta": map[string]any{"toolUse": map[string]any{"input": `{"pa`}}}),
		bedrockEventFrame("contentBlockDelta", map[string]any{"contentBlockIndex": 1, "delta": map[string]any{"toolUse": map[string]any{"input": `th":"a"}`}}}),
		bedrockEventFrame("contentBlockStop", map[string]any{"contentBlockIndex": 1}),
		bedrockEventFrame("contentBlockDelta", map[string]any{"contentBlockIndex": 2, "delta": map[string]any{"reasoningContent": map[string]any{"text": "think"}}}),
		bedrockEventFrame("contentBlockDelta", map[string]any{"contentBlockIndex": 2, "delta": map[string]any{"reasoningContent": map[string]any{"signature": "sig"}}}),
		bedrockEventFrame("contentBlockStop", map[string]any{"contentBlockIndex": 2}),
		bedrockEventFrame("messageStop", map[string]any{"stopReason": "tool_use"}),
		bedrockEventFrame("metadata", map[string]any{"usage": map[string]any{
			"inputTokens": 100, "outputTokens": 20, "cacheReadInputTokens": 30, "cacheWriteInputTokens": 10,
			"totalTokens": 160,
			"cacheDetails": []any{
				map[string]any{"ttl": "1h", "inputTokens": 4},
				map[string]any{"ttl": "5m", "inputTokens": 6},
			},
		}}),
	}
	var captured http.Request
	var payload map[string]any
	server := bedrockTestServer(t, frames, &captured, &payload)

	model := bedrockTestModel(server.URL)
	var providerEventCount int
	stream := StreamBedrockConverse(model, TranscriptContext{Messages: []Message{
		&SystemMessage{Content: StringOrBlocks{Text: "be helpful"}},
		&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
	}}, &BedrockOptions{
		APIKey: "bearer-token", Env: ProviderEnv{
			"AWS_ACCESS_KEY_ID": "AKID", "AWS_SECRET_ACCESS_KEY": "SECRET",
		},
		OnProviderStreamEvent: func(data json.RawMessage, eventModel *Model) {
			if eventModel != model || !json.Valid(data) {
				t.Errorf("provider callback data/model = %s/%p", data, eventModel)
			}
			providerEventCount++
		},
		BearerToken: "bearer-token",
		Reasoning:   ThinkHigh,
	})
	message, err := stream.Result(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if message.StopReason != StopToolUse || message.RawStopReason == nil || *message.RawStopReason != "tool_use" {
		t.Fatalf("message = %+v", message)
	}
	if providerEventCount != len(frames) {
		t.Fatalf("provider callback count = %d; want %d", providerEventCount, len(frames))
	}
	if len(message.Content) != 3 {
		t.Fatalf("content = %+v", message.Content)
	}
	if text, ok := message.Content[0].(TextContent); !ok || text.Text != "Hello" {
		t.Fatalf("text = %+v", message.Content[0])
	}
	call, ok := message.Content[1].(ToolCall)
	if !ok || call.ID != "call-1" || call.Name != "read" || string(call.Arguments) != `{"path":"a"}` {
		t.Fatalf("tool call = %+v", message.Content[1])
	}
	thinking, ok := message.Content[2].(ThinkingContent)
	if !ok || thinking.Thinking != "think" || thinking.ThinkingSignature == nil || *thinking.ThinkingSignature != "sig" {
		t.Fatalf("thinking = %+v", message.Content[2])
	}
	// Usage includes the one-hour cache write split.
	if message.Usage.Input != 100 || message.Usage.Output != 20 || message.Usage.CacheRead != 30 ||
		message.Usage.CacheWrite != 10 || message.Usage.TotalTokens != 160 {
		t.Fatalf("usage = %+v", message.Usage)
	}
	if message.Usage.CacheWrite1h == nil || *message.Usage.CacheWrite1h != 4 {
		t.Fatalf("cacheWrite1h = %v", message.Usage.CacheWrite1h)
	}
	// Cost is computed from the model rates; only assert it is additive.
	if message.Usage.Cost.Total != message.Usage.Cost.Input+message.Usage.Cost.Output+
		message.Usage.Cost.CacheRead+message.Usage.Cost.CacheWrite {
		t.Fatalf("cost = %+v", message.Usage.Cost)
	}

	// Bearer auth is used instead of SigV4 and the request shape is right.
	if captured.Header.Get("Authorization") != "Bearer bearer-token" {
		t.Fatalf("authorization = %q", captured.Header.Get("Authorization"))
	}
	if !strings.HasSuffix(captured.URL.Path, "/model/"+model.ID+"/converse-stream") {
		t.Fatalf("path = %s", captured.URL.Path)
	}
	// The AWS SDK binds modelId to the HTTP path, not the JSON body.
	if _, exists := payload["modelId"]; exists {
		t.Fatalf("payload = %#v", payload)
	}
	system, ok := payload["system"].([]any)
	if !ok || len(system) == 0 {
		t.Fatalf("payload = %#v", payload["system"])
	}
	// The Claude model gets a cache point on the system prompt.
	if point, _ := system[len(system)-1].(map[string]any)["cachePoint"].(map[string]any); point["type"] != "default" {
		t.Fatalf("system = %#v", system)
	}
	additional, _ := payload["additionalModelRequestFields"].(map[string]any)
	if additional["thinking"] == nil {
		t.Fatalf("payload = %#v", payload["additionalModelRequestFields"])
	}
}

func TestBedrockStreamSigV4AndErrors(t *testing.T) {
	frames := [][]byte{bedrockEventFrame("messageStart", map[string]any{"role": "assistant"})}
	var captured http.Request
	server := bedrockTestServer(t, frames, &captured, nil)

	model := bedrockTestModel(server.URL)
	stream := StreamBedrockConverse(model, TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}, &BedrockOptions{Env: ProviderEnv{
		"AWS_ACCESS_KEY_ID": "AKID", "AWS_SECRET_ACCESS_KEY": "SECRET", "AWS_REGION": "us-west-2",
	}})
	message, err := stream.Result(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// The stream ended without a stop reason after the start event.
	if message.StopReason != StopError || message.ErrorMessage == nil ||
		!strings.Contains(*message.ErrorMessage, "without a stop reason") {
		t.Fatalf("message = %+v", message)
	}
	// SigV4 signed the request with the configured region.
	authorization := captured.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "AWS4-HMAC-SHA256 Credential=AKID/") ||
		!strings.Contains(authorization, "/us-west-2/bedrock/aws4_request") {
		t.Fatalf("authorization = %q", authorization)
	}

	// A modeled exception event maps through the error prefixes and records the
	// failure diagnostic.
	exceptionFrames := [][]byte{
		bedrockEventFrame("messageStart", map[string]any{"role": "assistant"}),
		buildAWSEventStreamMessage(nil, map[string]any{
			":message-type":   "exception",
			":exception-type": "ThrottlingException",
			":content-type":   "application/json",
		}, []byte(`{"message":"rate exceeded"}`)),
	}
	exceptionServer := bedrockTestServer(t, exceptionFrames, nil, nil)
	stream = StreamBedrockConverse(bedrockTestModel(exceptionServer.URL), TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}, &BedrockOptions{BearerToken: "token"})
	message, _ = stream.Result(context.Background())
	if message.StopReason != StopError || message.ErrorMessage == nil ||
		!strings.Contains(*message.ErrorMessage, "Throttling error: rate exceeded") {
		t.Fatalf("message = %+v", message)
	}
	if len(message.Diagnostics) != 1 || message.Diagnostics[0].Type != "bedrock_response_failure" {
		t.Fatalf("diagnostics = %+v", message.Diagnostics)
	}
	details := string(message.Diagnostics[0].Details)
	if !strings.Contains(details, `"errorCode":"ThrottlingException"`) ||
		!strings.Contains(details, `"requestId":"req-1"`) {
		t.Fatalf("details = %s", details)
	}

	// An unmodeled JSON service error uses the SDK's Unknown prefix and body message.
	failing := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("x-amzn-requestid", "req-2")
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"message":"no access"}`))
	}))
	defer failing.Close()
	stream = StreamBedrockConverse(bedrockTestModel(failing.URL), TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}, &BedrockOptions{BearerToken: "token"})
	message, _ = stream.Result(context.Background())
	if message.StopReason != StopError || message.ErrorMessage == nil ||
		*message.ErrorMessage != "Unknown: no access" {
		t.Fatalf("message = %+v", message)
	}
}

func TestBedrockStreamRedactedReasoning(t *testing.T) {
	// Encrypted reasoning arrives as opaque bytes and is replayed as
	// redactedContent with a base64 signature.
	redacted := []byte{1, 2, 3}
	frames := [][]byte{
		bedrockEventFrame("messageStart", map[string]any{"role": "assistant"}),
		bedrockEventFrame("contentBlockDelta", map[string]any{"contentBlockIndex": 0,
			"delta": map[string]any{"reasoningContent": map[string]any{"redactedContent": base64.StdEncoding.EncodeToString(redacted)}}}),
		bedrockEventFrame("contentBlockStop", map[string]any{"contentBlockIndex": 0}),
		bedrockEventFrame("messageStop", map[string]any{"stopReason": "end_turn"}),
	}
	server := bedrockTestServer(t, frames, nil, nil)
	stream := StreamBedrockConverse(bedrockTestModel(server.URL), TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}, &BedrockOptions{BearerToken: "token"})
	message, _ := stream.Result(context.Background())
	if message.StopReason != StopStop || len(message.Content) != 1 {
		t.Fatalf("message = %+v", message)
	}
	thinking, ok := message.Content[0].(ThinkingContent)
	if !ok || !thinking.Redacted || thinking.Thinking != "[Reasoning redacted]" {
		t.Fatalf("thinking = %+v", message.Content[0])
	}
	if thinking.ThinkingSignature == nil || *thinking.ThinkingSignature != base64.StdEncoding.EncodeToString(redacted) {
		t.Fatalf("signature = %v", thinking.ThinkingSignature)
	}

	// The stored signature replays as redactedContent, and a non-base64
	// signature is dropped rather than failing the request.
	body := map[string]any{}
	replayServer := bedrockTestServer(t, nil, nil, &body)
	replayModel := bedrockTestModel(replayServer.URL)
	stream = StreamBedrockConverse(replayModel, TranscriptContext{Messages: []Message{
		&AssistantMessage{
			Provider: replayModel.Provider, API: replayModel.API, Model: replayModel.ID,
			Content: ContentList{ThinkingContent{
				Thinking: "[Reasoning redacted]", Redacted: true,
				ThinkingSignature: new(base64.StdEncoding.EncodeToString(redacted)),
			}},
		},
	}}, &BedrockOptions{BearerToken: "token"})
	_, _ = stream.Result(context.Background())
	messages, _ := body["messages"].([]any)
	first, _ := messages[0].(map[string]any)
	blocks, _ := first["content"].([]any)
	block, _ := blocks[0].(map[string]any)
	reasoningContent, _ := block["reasoningContent"].(map[string]any)
	// Go marshals []byte as base64, which is the AWS blob encoding.
	encodedRedacted, _ := reasoningContent["redactedContent"].(string)
	if encodedRedacted != base64.StdEncoding.EncodeToString(redacted) {
		t.Fatalf("payload = %#v", body)
	}
}

func TestBedrockMessageStartValidation(t *testing.T) {
	frames := [][]byte{bedrockEventFrame("messageStart", map[string]any{"role": "user"})}
	server := bedrockTestServer(t, frames, nil, nil)
	stream := StreamBedrockConverse(bedrockTestModel(server.URL), TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}, &BedrockOptions{BearerToken: "token"})
	message, _ := stream.Result(context.Background())
	if message.StopReason != StopError || message.ErrorMessage == nil ||
		!strings.Contains(*message.ErrorMessage, "Unexpected assistant message start") {
		t.Fatalf("message = %+v", message)
	}
}

func (c staticAuthContext) Env(name string) (string, bool) {
	value, ok := c[name]
	return value, ok && value != ""
}

func (c staticAuthContext) FileExists(string) bool { return false }

// pi v1.4.0 (4a080252), api/bedrock-converse-stream.ts wraps payload, response
// and provider-event callbacks in the stream's try/catch. A host callback error
// fails this stream rather than terminating the application. Re-exec isolates
// the previously unrecovered provider-goroutine panic from the test runner.
func TestBedrockCallbackFailuresStayInStream(t *testing.T) {
	const probeEnv = "PIER_TEST_BEDROCK_CALLBACK_FAILURE"
	if mode := os.Getenv(probeEnv); mode != "" {
		frames := [][]byte{
			bedrockEventFrame("messageStart", map[string]any{"role": "assistant"}),
			bedrockEventFrame("messageStop", map[string]any{"stopReason": "end_turn"}),
		}
		server := bedrockTestServer(t, frames, nil, nil)
		options := &BedrockOptions{
			APIKey: "test-key", Env: ProviderEnv{"AWS_REGION": "us-east-1"},
			BearerToken: "test-key",
		}
		switch mode {
		case "payload":
			options.OnPayload = func(json.RawMessage, *Model) json.RawMessage { panic(errors.New("hook failed")) }
		case "response":
			options.OnResponse = func(ProviderResponse, *Model) { panic(errors.New("hook failed")) }
		case "event":
			options.OnProviderStreamEvent = func(json.RawMessage, *Model) { panic(errors.New("hook failed")) }
		case "control":
		default:
			t.Fatalf("unknown probe mode %q", mode)
		}
		message, err := StreamBedrockConverse(bedrockTestModel(server.URL), helloUserContext(), options).Result(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if mode == "control" {
			if message.StopReason != StopStop {
				t.Fatalf("control stream failed: %+v", message)
			}
		} else if message.StopReason != StopError || message.ErrorMessage == nil || *message.ErrorMessage != "hook failed" {
			t.Fatalf("callback failure did not become a stream error: %+v", message)
		}
		return
	}
	for _, mode := range []string{"control", "payload", "response", "event"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBedrockCallbackFailuresStayInStream$", "-test.timeout=10s")
			command.Env = append(os.Environ(), probeEnv+"="+mode)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("callback %s terminated the process: %v\n%s", mode, err, output)
			}
		})
	}
}
