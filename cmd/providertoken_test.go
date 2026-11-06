package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

// providerTokenForMCP hands a `/login` provider's API key (or its OAuth access
// token, which auth resolution stores as the key) to an MCP server configured
// with `auth: { provider }` (upstream extensions/mcp/index.ts providerToken:
// modelRegistry.getApiKeyForProvider). Four boundaries: no runtime, an unknown
// provider, a resolvable key and an unresolvable one.
func TestProviderTokenForMCP(t *testing.T) {
	isolatedAgentDir(t)
	models := `{"providers":{` +
		`"envprov":{"baseUrl":"http://127.0.0.1:1/v1","api":"openai-completions","apiKey":"$PROBE_KEY","models":[{"id":"m"}]},` +
		`"cmdprov":{"baseUrl":"http://127.0.0.1:1/v1","api":"openai-completions","apiKey":"!sh -c 'exit 1'","models":[{"id":"m"}]}}}`
	if err := os.WriteFile(filepath.Join(coding.GetAgentDir(), "models.json"), []byte(models), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROBE_KEY", "sk-probe")
	runtime, err := coding.CreateModelRuntime(coding.CreateModelRuntimeOptions{AllowModelNetwork: false})
	if err != nil {
		t.Fatal(err)
	}

	if token, err := providerTokenForMCP(runtime, "envprov"); err != nil || token != "sk-probe" {
		t.Fatalf("envprov = %q, %v", token, err)
	}
	// An unknown provider has no credential, which is not an error.
	if token, err := providerTokenForMCP(runtime, "nope"); err != nil || token != "" {
		t.Fatalf("unknown provider = %q, %v", token, err)
	}
	if token, err := providerTokenForMCP(nil, "envprov"); err != nil || token != "" {
		t.Fatalf("nil runtime = %q, %v", token, err)
	}
	// A provider whose key cannot be resolved reports why instead of quietly
	// sending no credential (D384 records the difference from the reference).
	token, err := providerTokenForMCP(runtime, "cmdprov")
	if token != "" || err == nil || !strings.Contains(err.Error(), "API key auth failed for provider cmdprov") {
		t.Fatalf("unresolvable provider = %q, %v", token, err)
	}
}
