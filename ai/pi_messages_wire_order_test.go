package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), packages/ai/src/api/pi-messages.ts stream
// serializes {model, context, options}; it does not decode the transcript into
// unordered maps before calling onPayload or sending the request.
func TestPiMessagesRequestPreservesMemberOrder(t *testing.T) {
	bodies := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		bodies <- string(body)
		writer.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	payloads := make(chan string, 1)
	stream := StreamPiMessages(piMessagesModel(server.URL), piMessagesContext(), &PiMessagesOptions{
		APIKey: "token", Temperature: new(0.5), MaxTokens: new(16),
		CacheRetention: CacheRetentionLong, SessionID: "session-1",
		OnPayload: func(payload json.RawMessage, _ *Model) json.RawMessage {
			payloads <- string(payload)
			return nil
		},
		Reasoning: "high", ToolChoice: json.RawMessage(`{"type":"function","name":"lookup"}`),
	})
	if _, err := stream.Result(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := `{"model":"balanced","context":{"messages":[{"role":"user","content":"hello","timestamp":1}]},"options":{"temperature":0.5,"maxTokens":16,"reasoning":"high","cacheRetention":"long","sessionId":"session-1","toolChoice":{"type":"function","name":"lookup"}}}`
	for _, got := range []string{<-payloads, <-bodies} {
		if got != want {
			t.Fatalf("payload = %s\nwant      %s", got, want)
		}
	}
}

// Reference: pi-messages.ts stream accepts any defined onPayload return value,
// then JSON.stringify serializes that value without an object-map round trip.
func TestPiMessagesSendsPayloadReplacementVerbatim(t *testing.T) {
	for _, replacement := range []string{
		`{"z":1,"a":{"z":2,"a":3}}`,
		`[{"z":1,"a":2}]`,
		`"replacement"`,
		`42`,
		`null`,
	} {
		t.Run(replacement, func(t *testing.T) {
			bodies := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, _ := io.ReadAll(request.Body)
				bodies <- string(body)
				writer.WriteHeader(http.StatusBadRequest)
			}))
			defer server.Close()
			stream := StreamPiMessages(piMessagesModel(server.URL), piMessagesContext(), &PiMessagesOptions{
				APIKey: "token",
				OnPayload: func(json.RawMessage, *Model) json.RawMessage {
					return json.RawMessage(replacement)
				},
			})
			if _, err := stream.Result(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := <-bodies; got != replacement {
				t.Fatalf("body = %s, want %s", got, replacement)
			}
		})
	}
}
