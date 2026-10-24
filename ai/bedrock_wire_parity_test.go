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

// Reference: pi v1.4.0 (4a080252), api/bedrock-converse-stream.ts and its
// AWS SDK ConverseStream serializer. SDK command params are not HTTP bodies.
// D268: only the observed hook JSON normalizes native binary values to base64.
// Regenerate: node --experimental-strip-types testdata/bedrock-wire/generate.mjs ../pi > testdata/bedrock-wire/golden.json
func TestBedrockRequestsMatchSDKWire(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock-wire/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name        string            `json:"name"`
		ModelID     string            `json:"modelId"`
		ModelName   string            `json:"modelName"`
		Messages    []json.RawMessage `json:"messages"`
		Options     BedrockOptions    `json:"options"`
		Replacement json.RawMessage   `json:"replacement"`
		Payload     string            `json:"payload"`
		Path        string            `json:"path"`
		Body        string            `json:"body"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			bodies := make(chan string, 1)
			paths := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				bodies <- string(raw)
				paths <- r.URL.EscapedPath()
				w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
				w.Write(bedrockEventFrame("messageStop", map[string]any{"stopReason": "end_turn"}))
			}))
			defer server.Close()
			model := bedrockTestModel(server.URL)
			if fixture.ModelID != "" {
				model.ID = fixture.ModelID
				model.Name = fixture.ModelName
			}
			model.Compat = &ModelCompat{Bedrock: &BedrockCompat{SupportsStrictMode: new(true)}}
			var messages []Message
			for _, raw := range fixture.Messages {
				message, err := UnmarshalMessage(raw)
				if err != nil {
					t.Fatal(err)
				}
				messages = append(messages, message)
			}
			options := fixture.Options
			options.Env = ProviderEnv{"AWS_BEDROCK_SKIP_AUTH": "1"}
			payloads := make(chan string, 1)
			options.OnPayload = func(p json.RawMessage, _ *Model) json.RawMessage { payloads <- string(p); return fixture.Replacement }
			result, err := StreamBedrockConverse(model, TranscriptContext{Messages: messages}, &options).Result(context.Background())
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
				t.Errorf("body = %s\nwant   %s", got, fixture.Body)
			}
		})
	}
}
