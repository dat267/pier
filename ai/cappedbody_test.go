package ai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dat267/pier/internal/boundedio"
)

// D341 caps a successful remote JSON body and fails as a protocol error once it
// exceeds the limit. The Radius gateway config and the OpenRouter image
// response are such documents; both read a silent prefix and accepted the
// document whenever its JSON ended before the cap (A429).
func TestRadiusGatewayConfigOverTheCapIsAProtocolError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"models":[]}` + strings.Repeat(" ", (1<<20)+1)))
	}))
	defer server.Close()

	config, err := LoadRadiusGatewayConfig(context.Background(), server.URL, "")
	if err == nil {
		t.Fatalf("an oversized gateway config was accepted (%+v)", config)
	}
	if !errors.Is(err, boundedio.ErrTooLarge) {
		t.Fatalf("err = %v; want the size-limit error (D341)", err)
	}
}

// The image generation response is capped at 8 MiB and must fail at that cap
// rather than parse a silent prefix (A429).
func TestOpenRouterImagesResponseOverTheCapIsAProtocolError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		body := `{"id":"gen-1","choices":[{"message":{"content":"ok"}}]}` + strings.Repeat(" ", (8<<20)+1)
		_, _ = writer.Write([]byte(body))
	}))
	defer server.Close()

	model := ImagesModel{
		ID: "google/gemini-2.5-flash-image", API: KnownImagesApiOpenRouter, Provider: "openrouter",
		BaseURL: server.URL,
	}
	result := OpenRouterImages{}.GenerateImages(&model, ImagesContext{
		Input: []ImagesInputContent{TextContent{Text: "cat"}},
	}, &ImagesOptions{APIKey: "k"})
	if result.StopReason != ImagesStopError {
		t.Fatalf("stop reason = %q; want an error", result.StopReason)
	}
	if result.ErrorMessage == nil || !strings.Contains(*result.ErrorMessage, "exceeds the size limit") {
		t.Fatalf("error message = %v; want the size-limit error (D341)", result.ErrorMessage)
	}
}
