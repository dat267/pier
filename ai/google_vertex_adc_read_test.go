package ai

import (
	ctxpkg "context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/internal/boundedio"
)

// vertexADCTokenAccess resolves an ADC access token from an authorized-user
// credential file whose token endpoint is tokenURL.
func vertexADCTokenAccess(t *testing.T, tokenURL string) (string, error) {
	t.Helper()
	credentialsPath := filepath.Join(t.TempDir(), "adc.json")
	encoded, err := json.Marshal(map[string]any{
		"type": "authorized_user", "client_id": "client-id", "client_secret": "client-secret",
		"refresh_token": "refresh-token", "token_uri": tokenURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(credentialsPath, encoded, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialsPath)
	vertexTokenCache.mu.Lock()
	vertexTokenCache.entries = map[string]cachedVertexToken{}
	vertexTokenCache.mu.Unlock()
	return vertexAccessToken(ctxpkg.Background(), providerEnvMap())
}

// D341 caps a successful remote JSON body and fails as a protocol error once it
// exceeds the limit: 1 MiB for OAuth documents. The authorized-user token
// response is such a document, so an oversized one must report the size limit
// instead of reading a silent 1 MiB prefix, which accepts the token when the
// JSON ends before the cap (A426).
func TestVertexADCTokenResponseOverTheLimitIsAProtocolError(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		body := `{"access_token":"adc-token","expires_in":3600}` + strings.Repeat(" ", (1<<20)+1)
		_, _ = writer.Write([]byte(body))
	}))
	defer tokenServer.Close()

	token, err := vertexADCTokenAccess(t, tokenServer.URL)
	if err == nil {
		t.Fatalf("an oversized token response was accepted (token %q)", token)
	}
	if !errors.Is(err, boundedio.ErrTooLarge) {
		t.Fatalf("err = %v; want the size-limit error (D341)", err)
	}
}

// A token response cut off mid-body must surface the transport error, as the
// instance-metadata path in the same file does, instead of a JSON parse error
// that hides whether the network dropped or the server sent garbage (A426).
func TestVertexADCTokenResponseCutOffSurfacesTheReadError(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Content-Length", "64")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"access_token":"adc`))
	}))
	defer tokenServer.Close()

	token, err := vertexADCTokenAccess(t, tokenServer.URL)
	if err == nil {
		t.Fatalf("a cut-off token response was accepted (token %q)", token)
	}
	if !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("err = %v; want the transport error", err)
	}
}

// Control for the cap split: an error response still reports its status and
// body, which the token path builds before it looks at a successful document.
func TestVertexADCTokenErrorResponseReportsStatusAndBody(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`))
	}))
	defer tokenServer.Close()

	_, err := vertexADCTokenAccess(t, tokenServer.URL)
	if err == nil {
		t.Fatal("a rejected token response produced a token")
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("err = %v; want the status and body", err)
	}
}
