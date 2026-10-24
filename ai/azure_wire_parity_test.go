package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), api/azure-openai-responses.ts stream and
// buildParams, including the OpenAI SDK HTTP boundary.
// Regenerate: node --experimental-strip-types testdata/azure-wire/generate.mjs ../pi > testdata/azure-wire/golden.json
func TestAzureRequestsMatchUpstreamWire(t *testing.T) {
	data, err := os.ReadFile("testdata/azure-wire/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string            `json:"name"`
		Messages []json.RawMessage `json:"messages"`
		Options  struct {
			AzureOpenAIResponsesOptions
			ReasoningEffort  ThinkingLevel   `json:"reasoningEffort"`
			ReasoningSummary string          `json:"reasoningSummary"`
			ToolChoice       json.RawMessage `json:"toolChoice"`
		} `json:"options"`
		Replacement json.RawMessage `json:"replacement"`
		Payload     string          `json:"payload"`
		Path        string          `json:"path"`
		APIVersion  string          `json:"apiVersion"`
		Body        string          `json:"body"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			bodies := make(chan string, 1)
			paths := make(chan string, 1)
			versions := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				bodies <- string(raw)
				paths <- r.URL.Path
				versions <- r.URL.Query().Get("api-version")
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":0,\"output_tokens\":0,\"total_tokens\":0}}}\n\n")
			}))
			defer server.Close()
			model := &Model{API: APIAzureOpenAIResponses, ID: "gpt-5", Provider: "azure", BaseURL: server.URL + "/openai/v1", Reasoning: true, MaxTokens: 8192, ContextWindow: 128000, Input: []string{"text", "image"}}
			var messages []Message
			for _, raw := range fixture.Messages {
				message, err := UnmarshalMessage(raw)
				if err != nil {
					t.Fatal(err)
				}
				messages = append(messages, message)
			}
			options := fixture.Options.AzureOpenAIResponsesOptions
			options.ReasoningEffort = fixture.Options.ReasoningEffort
			options.ReasoningSummary = fixture.Options.ReasoningSummary
			options.ToolChoice = fixture.Options.ToolChoice
			options.APIKey = "token"
			options.Env = ProviderEnv{}
			options.AzureBaseURL = strings.ReplaceAll(options.AzureBaseURL, "https://invalid.test", server.URL)
			payloads := make(chan string, 1)
			options.OnPayload = func(p json.RawMessage, _ *Model) json.RawMessage { payloads <- string(p); return fixture.Replacement }
			result, err := StreamAzureOpenAIResponses(model, TranscriptContext{Messages: messages}, &options).Result(context.Background())
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
			if got := <-versions; got != fixture.APIVersion {
				t.Errorf("api-version = %s, want %s", got, fixture.APIVersion)
			}
			if got := <-bodies; got != fixture.Body {
				t.Errorf("body = %s\nwant   %s", got, fixture.Body)
			}
		})
	}
}
