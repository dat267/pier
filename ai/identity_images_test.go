package ai

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Reference: pi v1.4.0 api/openrouter-images.ts applies identity defaults
// beneath model and caller headers, including null removals.
func TestImageRequestIdentityHeaders(t *testing.T) {
	for _, mode := range []string{"identity", "model override", "request override", "remove"} {
		t.Run(mode, func(t *testing.T) {
			captured := make(chan http.Header, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured <- r.Header.Clone()
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"header probe"}}`))
			}))
			defer server.Close()
			model := &ImagesModel{ID: "image-model", API: KnownImagesApiOpenRouter, Provider: "openrouter", BaseURL: server.URL}
			options := &ImagesOptions{StreamOptions: StreamOptions{
				APIKey: "token", Identity: &ClientIdentity{UserAgent: "image-agent", Attribution: PIAttribution},
			}}
			wantAgent, wantTitle := "image-agent", "pi"
			if mode != "identity" {
				model.Headers = map[string]string{"User-Agent": "model-agent", "X-OpenRouter-Title": "model-title"}
				wantAgent, wantTitle = "model-agent", "model-title"
			}
			if mode == "request override" {
				options.Headers = ProviderHeaders{"user-agent": strPtr("request-agent"), "x-openrouter-title": strPtr("request-title")}
				wantAgent, wantTitle = "request-agent", "request-title"
			}
			if mode == "remove" {
				options.Headers = ProviderHeaders{"user-agent": nil, "x-openrouter-title": nil}
				wantAgent, wantTitle = "", ""
			}
			output := (OpenRouterImages{}).GenerateImages(model, ImagesContext{Input: []Content{TextContent{Text: "cat"}}}, options)
			if output.StopReason != ImagesStopError {
				t.Fatalf("probe result = %+v", output)
			}
			select {
			case headers := <-captured:
				if headers.Get("User-Agent") != wantAgent || headers.Get("X-OpenRouter-Title") != wantTitle {
					t.Fatalf("headers = %v; want agent %q, title %q", headers, wantAgent, wantTitle)
				}
			default:
				t.Fatalf("no HTTP request; output = %+v", output)
			}
		})
	}
}
