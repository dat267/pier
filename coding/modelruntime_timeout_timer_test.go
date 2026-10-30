package coding

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// The pinned core/model-runtime.ts schedules this deadline with setTimeout;
// Node clamps delays above 2^31-1ms to 1ms (D254).
func TestCreateModelRuntimeClampsLargeRefreshTimeoutLikeNode(t *testing.T) {
	previous, wasSet := os.LookupEnv("PI_OFFLINE")
	if err := os.Unsetenv("PI_OFFLINE"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv("PI_OFFLINE", previous)
		} else {
			_ = os.Unsetenv("PI_OFFLINE")
		}
	})

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()

	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	timeoutMS := int64(2_147_483_648) // Node setTimeout clamps this to 1ms.
	credentials := newMemoryCredentialStore()
	credentials.entries["anthropic"] = &ai.Credential{
		Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "local-test-key"},
	}
	done := make(chan error, 1)
	go func() {
		_, err := CreateModelRuntime(CreateModelRuntimeOptions{
			Credentials: credentials, ModelsStore: NewInMemoryCodingAgentModelsStore(),
			DisableModelsJSON: true, AllowModelNetwork: true, CatalogBaseURL: server.URL,
			ModelRefreshTimeoutMS: &timeoutMS, Signal: parent, RefreshOnCreate: new(true),
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("CreateModelRuntime() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("runtime creation did not stop after parent cancellation")
		}
		t.Fatal("large model refresh timeout did not clamp to Node's 1ms timer")
	}
}
