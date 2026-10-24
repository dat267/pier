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

// Reference: pi v1.4.0 (4a080252), api/google-generative-ai.ts and
// api/google-vertex.ts buildParams/stream, plus @google/genai 2.21.0 wire
// serializers. Fixtures capture HTTP bodies, not the SDK's input params.
// Regenerate: node --experimental-strip-types testdata/google-wire/generate.mjs ../pi > testdata/google-wire/golden.json
func TestGoogleRequestsMatchSDKWire(t *testing.T) {
	data, err := os.ReadFile("testdata/google-wire/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		API         Api               `json:"api"`
		Name        string            `json:"name"`
		Messages    []json.RawMessage `json:"messages"`
		Options     GoogleOptions     `json:"options"`
		Body        string            `json:"body"`
		Payload     string            `json:"payload"`
		Replacement json.RawMessage   `json:"replacement"`
		Path        string            `json:"path"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(string(fixture.API)+"/"+fixture.Name, func(t *testing.T) {
			bodies := make(chan string, 1)
			paths := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				paths <- request.URL.Path
				body, _ := io.ReadAll(request.Body)
				bodies <- string(body)
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(writer, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}],\"role\":\"model\"},\"finishReason\":\"STOP\"}]}\n\n")
			}))
			defer server.Close()
			model := &Model{API: fixture.API, ID: "gemini-3-pro", Provider: "google", BaseURL: server.URL + "/v1",
				Reasoning: true, MaxTokens: 1000, ContextWindow: 10000, Input: []string{"text", "image"}}
			var messages []Message
			for _, raw := range fixture.Messages {
				message, err := UnmarshalMessage(raw)
				if err != nil {
					t.Fatal(err)
				}
				messages = append(messages, message)
			}
			options := fixture.Options
			payloads := make(chan string, 1)
			options.OnPayload = func(payload json.RawMessage, _ *Model) json.RawMessage {
				payloads <- string(payload)
				return fixture.Replacement
			}
			options.APIKey = "token"
			options.Env = ProviderEnv{}
			var stream *AssistantMessageEventStream
			if fixture.API == APIGoogleVertex {
				model.Provider = "google-vertex"
				stream = StreamGoogleVertex(model, TranscriptContext{Messages: messages}, &GoogleVertexOptions{
					GoogleOptions: options, Project: "project", Location: "global",
				})
			} else {
				stream = StreamGoogleGenerativeAI(model, TranscriptContext{Messages: messages}, &options)
			}
			result, err := stream.Result(context.Background())
			if err != nil {
				t.Fatal(err)
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
