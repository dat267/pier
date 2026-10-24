package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

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
