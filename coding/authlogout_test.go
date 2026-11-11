package coding

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// storeAuthCredential writes a credential into the test agent's auth.json.
func storeAuthCredential(t *testing.T, agentDir, providerID string, credential *ai.Credential) {
	t.Helper()
	storage := NewAuthStorage(filepath.Join(agentDir, "auth.json"))
	if _, err := storage.Modify(providerID, func(*ai.Credential) (*ai.Credential, error) {
		return credential, nil
	}, context.Background()); err != nil {
		t.Fatal(err)
	}
}

func runLogoutCommand(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	handled, code := RunAuthCommandNamed("pier", append([]string{"auth", "logout"}, args...), &stdout, &stderr)
	if !handled {
		t.Fatal("auth logout was not handled")
	}
	return code, stdout.String(), stderr.String()
}

// pi v1.5.2 (b4afdddf) cli/auth-logout.ts: `pi auth logout <provider>` removes
// the credential the login stored, matching the provider by id or name, and says
// which kind it removed. Environment variables and models.json stay.
func TestAuthLogoutRemovesStoredCredentials(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	storeAuthCredential(t, agentDir, "anthropic", &ai.Credential{
		Type: ai.CredentialOAuth,
		OAuth: &ai.OAuthCredential{OAuthCredentials: ai.OAuthCredentials{
			Access: "access", Refresh: "refresh", Expires: time.Now().Add(time.Hour).UnixMilli(),
		}},
	})
	storeAuthCredential(t, agentDir, "openai", &ai.Credential{
		Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "sk-test"},
	})

	code, stdout, stderr := runLogoutCommand(t, "Anthropic")
	if code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if stdout != "Logged out of Anthropic.\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	data, err := os.ReadFile(filepath.Join(agentDir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]json.RawMessage
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if _, kept := stored["anthropic"]; kept {
		t.Fatalf("anthropic credential still stored: %s", data)
	}
	if _, kept := stored["openai"]; !kept {
		t.Fatalf("openai credential removed too: %s", data)
	}

	code, stdout, _ = runLogoutCommand(t, "openai")
	if code != 0 || stdout != "Removed stored API key for OpenAI. Environment variables and models.json config are unchanged.\n" {
		t.Fatalf("api key logout = %d, %q", code, stdout)
	}

	// Nothing stored for the provider: exit 0, nothing removed.
	code, stdout, _ = runLogoutCommand(t, "anthropic")
	if code != 0 || !strings.HasPrefix(stdout, "No stored credentials for Anthropic.") {
		t.Fatalf("second logout = %d, %q", code, stdout)
	}
}

// An unknown provider lists what is stored, and a missing argument prints the
// usage line, both with exit 1.
func TestAuthLogoutRejectsUnknownProvidersAndMissingArguments(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	storeAuthCredential(t, agentDir, "anthropic", &ai.Credential{
		Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "sk-test"},
	})

	code, _, stderr := runLogoutCommand(t, "nope")
	if code != 1 || !strings.Contains(stderr, `Unknown provider "nope".`) {
		t.Fatalf("unknown provider = %d, %q", code, stderr)
	}
	if !strings.Contains(stderr, "Stored credentials: anthropic") {
		t.Fatalf("stored credentials not listed: %q", stderr)
	}

	code, _, stderr = runLogoutCommand(t)
	if code != 1 || !strings.Contains(stderr, "Usage: pier auth logout <provider>") {
		t.Fatalf("missing provider = %d, %q", code, stderr)
	}
}
