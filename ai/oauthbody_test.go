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

// D341 requires a successful remote JSON document to be read through
// internal/boundedio and to fail as a protocol error once it exceeds its limit:
// 1 MiB for OAuth documents. The provider OAuth flows built their own bounded
// read and discarded its error, so an oversized document whose JSON ends before
// the cap was accepted and a cut-off one reported a JSON parse error (A427).
func TestOAuthDocumentOverTheCapIsAProtocolError(t *testing.T) {
	fastDeviceCodeFlows(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		body := `{"device_code":"device-1","user_code":"CODE","verification_uri":"https://x.ai/device","expires_in":600,"interval":1}` +
			strings.Repeat(" ", (1<<20)+1)
		_, _ = writer.Write([]byte(body))
	}))
	defer server.Close()

	withXaiURLs(t, server.URL, func() {
		_, err := LoginXai(&AuthInteraction{Ctx: context.Background()})
		if err == nil {
			t.Fatal("an oversized OAuth document was accepted")
		}
		if !errors.Is(err, boundedio.ErrTooLarge) {
			t.Fatalf("err = %v; want the size-limit error (D341)", err)
		}
	})
}

// A document cut off mid-body must surface the transport error instead of a
// JSON parse error that hides whether the network dropped (A427).
func TestOAuthDocumentCutOffSurfacesTheReadError(t *testing.T) {
	fastDeviceCodeFlows(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Content-Length", "64")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"device_code":"device-1`))
	}))
	defer server.Close()

	withXaiURLs(t, server.URL, func() {
		_, err := LoginXai(&AuthInteraction{Ctx: context.Background()})
		if err == nil {
			t.Fatal("a cut-off OAuth document was accepted")
		}
		if !strings.Contains(err.Error(), "unexpected EOF") {
			t.Fatalf("err = %v; want the transport error", err)
		}
	})
}
