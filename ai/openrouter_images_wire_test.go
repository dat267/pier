package ai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// imagesResultProbe reads the fixture timestamp so the generated timestamp does
// not make the result comparison flaky.
type imagesResultProbe struct {
	Timestamp int64 `json:"timestamp"`
}

// Reference: pi v1.4.0 (4a080252), api/openrouter-images.ts buildParams and
// onPayload handling plus the AssistantImages shape (the params object literal
// fixes the body member order, a replacement is sent as-is, and output blocks
// carry their type discriminator with the OpenRouter construction order).
// Regenerate: node --experimental-strip-types testdata/openrouter-images/generate.mjs ../pi > testdata/openrouter-images/golden.json
func TestOpenRouterImagesWireMatchesUpstream(t *testing.T) {
	data, err := os.ReadFile("testdata/openrouter-images/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name        string            `json:"name"`
		Model       ImagesModel       `json:"model"`
		Input       []json.RawMessage `json:"input"`
		Replacement json.RawMessage   `json:"replacement"`
		Path        string            `json:"path"`
		Body        string            `json:"body"`
		Result      string            `json:"result"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			bodies := make(chan string, 1)
			paths := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				bodies <- string(b)
				paths <- r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"id":"r","choices":[{"message":{"content":"ok","images":[{"image_url":"data:image/png;base64,QUJD"}]}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
			}))
			defer server.Close()
			model := fixture.Model
			model.BaseURL = strings.ReplaceAll(model.BaseURL, "https://invalid.test", server.URL)
			var input []ImagesInputContent
			for _, raw := range fixture.Input {
				var probe struct {
					Type     string `json:"type"`
					Text     string `json:"text"`
					Data     string `json:"data"`
					MimeType string `json:"mimeType"`
				}
				if err := json.Unmarshal(raw, &probe); err != nil {
					t.Fatal(err)
				}
				if probe.Type == "text" {
					input = append(input, TextContent{Text: probe.Text})
				} else {
					input = append(input, ImageContent{Data: probe.Data, MimeType: probe.MimeType})
				}
			}
			options := &ImagesOptions{StreamOptions: StreamOptions{APIKey: "token"}}
			if len(fixture.Replacement) > 0 {
				replacement := fixture.Replacement
				options.OnPayload = func(json.RawMessage, *Model) json.RawMessage { return replacement }
			}
			result := OpenRouterImages{}.GenerateImages(&model, ImagesContext{Input: input}, options)
			if result.StopReason != ImagesStopStop {
				t.Fatalf("result = %+v", result)
			}
			if got := <-paths; got != fixture.Path {
				t.Errorf("path = %s, want %s", got, fixture.Path)
			}
			if got := <-bodies; got != fixture.Body {
				t.Errorf("body = %s\nwant   %s", got, fixture.Body)
			}
			var probe imagesResultProbe
			if err := json.Unmarshal([]byte(fixture.Result), &probe); err != nil {
				t.Fatal(err)
			}
			result.Timestamp = probe.Timestamp
			encoded, err := MarshalJSON(*result)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := string(encoded), fixture.Result; got != want {
				t.Errorf("result = %s\nwant     %s", got, want)
			}
		})
	}
}
