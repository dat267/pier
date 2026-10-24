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

// Reference: pi v1.4.0 (4a080252), api/openrouter-images.ts buildParams and
// onPayload handling. The params object literal fixes the body member order and
// a replacement is sent as-is.
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
				io.WriteString(w, `{"id":"r","choices":[{"message":{"content":"ok"}}]}`)
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
		})
	}
}
