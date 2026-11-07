package coding

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 cli/auth-check.ts createAuthCheckModelRuntime calls
// ModelRuntime.create with no modelsPath, so the default models.json is loaded;
// a provider declared there is found and its literal, $ENV or !command key
// resolves. Pier passed DisableModelsJSON, so `pi auth check` reported
// provider_not_found for every models.json-only provider that `pi auth
// print-api-key` could resolve.
func TestAuthCheckSeesModelsJSONProviders(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	t.Setenv("PIER_AUTH_TEST_KEY", "env-secret")
	models := `{"providers":{
		"lit":{"baseUrl":"http://127.0.0.1:1/v1","api":"openai-completions","apiKey":"dummy","models":[{"id":"l1","name":"L","contextWindow":1000,"maxTokens":100}]},
		"envprov":{"baseUrl":"http://127.0.0.1:1/v1","api":"openai-completions","apiKey":"$PIER_AUTH_TEST_KEY","models":[{"id":"e1","name":"E","contextWindow":1000,"maxTokens":100}]},
		"nokey":{"baseUrl":"http://127.0.0.1:1/v1","api":"openai-completions","models":[{"id":"n1","name":"N","contextWindow":1000,"maxTokens":100}]}
	}}`
	if err := os.WriteFile(filepath.Join(agentDir, "models.json"), []byte(models), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		args     []string
		status   string
		reason   string
		authType string
		exit     int
	}{
		{[]string{"auth", "check", "--provider", "lit", "--json"}, "ready", "", "api_key", 0},
		{[]string{"auth", "check", "--provider", "envprov", "--json"}, "ready", "", "api_key", 0},
		{[]string{"auth", "check", "--provider", "nokey", "--json"}, "not_ready", "credentials_not_configured", "", 1},
		{[]string{"auth", "check", "--model", "lit/l1", "--json"}, "ready", "", "api_key", 0},
	}
	for _, testCase := range cases {
		var stdout, stderr bytes.Buffer
		handled, code := RunAuthCommandNamed("pier", testCase.args, &stdout, &stderr)
		if !handled {
			t.Fatalf("%v was not handled", testCase.args)
		}
		if code != testCase.exit {
			t.Errorf("%v exit = %d, want %d (stdout %q stderr %q)", testCase.args, code, testCase.exit, stdout.String(), stderr.String())
		}
		var result struct {
			Status   string `json:"status"`
			Provider string `json:"provider"`
			Reason   string `json:"reason"`
			AuthType string `json:"authType"`
		}
		if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result); err != nil {
			t.Fatalf("%v output %q: %v (stderr %q)", testCase.args, stdout.String(), err, stderr.String())
		}
		if result.Status != testCase.status || result.Reason != testCase.reason || result.AuthType != testCase.authType {
			t.Errorf("%v = %+v, want status %q reason %q authType %q",
				testCase.args, result, testCase.status, testCase.reason, testCase.authType)
		}
		if strings.Contains(stdout.String(), "provider_not_found") {
			t.Errorf("%v reported provider_not_found for a models.json provider", testCase.args)
		}
	}
}
