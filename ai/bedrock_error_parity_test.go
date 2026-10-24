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

// Reference: pi v1.4.0 (4a080252), bedrock-converse-stream.ts catch and the
// pinned @aws-sdk/client-bedrock-runtime ConverseStream deserializer.
// Regenerate: node --experimental-strip-types testdata/bedrock-errors/generate.mjs ../pi > testdata/bedrock-errors/golden.json
func TestBedrockErrorsMatchPinnedSDK(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock-errors/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name      string `json:"name"`
		DRow      string `json:"dRow"`
		RequestID string `json:"requestId"`
		HTTP      *struct {
			Status      int    `json:"status"`
			ContentType string `json:"contentType"`
			Body        string `json:"body"`
		} `json:"http"`
		StreamError *struct {
			Member             string `json:"member"`
			Message            string `json:"message"`
			OriginalStatusCode int    `json:"originalStatusCode"`
			OriginalMessage    string `json:"originalMessage"`
		} `json:"streamError"`
		ErrorMessage      string         `json:"errorMessage"`
		StopReason        string         `json:"stopReason"`
		DiagnosticDetails map[string]any `json:"diagnosticDetails"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-amzn-requestid", fixture.RequestID)
				if fixture.StreamError != nil {
					w.Header().Set("content-type", "application/vnd.amazon.eventstream")
					frame := buildAWSEventStreamMessage(nil, map[string]any{
						":message-type":   "exception",
						":exception-type": "ModelStreamErrorException",
						":content-type":   "application/json",
					}, mustMarshalJSON(map[string]any{
						"message":            fixture.StreamError.Message,
						"originalStatusCode": fixture.StreamError.OriginalStatusCode,
						"originalMessage":    fixture.StreamError.OriginalMessage,
					}))
					_, _ = w.Write(frame)
					return
				}
				w.Header().Set("content-type", fixture.HTTP.ContentType)
				w.WriteHeader(fixture.HTTP.Status)
				_, _ = io.WriteString(w, fixture.HTTP.Body)
			}))
			defer server.Close()

			stream := StreamBedrockConverse(bedrockTestModel(server.URL), TranscriptContext{Messages: []Message{
				&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
			}}, &BedrockOptions{BearerToken: "test-token"})
			result, err := stream.Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.StopReason != fixture.StopReason {
				t.Errorf("stopReason = %q, want %q", result.StopReason, fixture.StopReason)
			}
			gotMessage := ""
			if result.ErrorMessage != nil {
				gotMessage = *result.ErrorMessage
			}
			if fixture.DRow == "D270" {
				if gotMessage != "502 Bad Gateway: proxy exploded" {
					t.Errorf("D270 Go message = %q, want documented raw-body output", gotMessage)
				}
			} else if gotMessage != fixture.ErrorMessage {
				t.Errorf("errorMessage = %q, want %q", gotMessage, fixture.ErrorMessage)
			}
			var details map[string]any
			if len(result.Diagnostics) > 0 {
				if err := json.Unmarshal(result.Diagnostics[0].Details, &details); err != nil {
					t.Fatal(err)
				}
			}
			if !equalJSONMaps(details, fixture.DiagnosticDetails) {
				t.Errorf("diagnostic details = %#v, want %#v", details, fixture.DiagnosticDetails)
			}
		})
	}
}
