package cmd

import (
	"context"
	"github.com/dat267/pier/coding"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --list-models resolves the model runtime before any background pass and
// reports what went wrong with the catalog: pi v1.5.0 main.ts drains the
// settings diagnostics and the models.json load error before printing the
// table, and still exits 0 when nothing is available.
func TestListModelsReportsCatalogProblems(t *testing.T) {
	isolatedAgentDir(t)
	if err := os.WriteFile(filepath.Join(coding.GetAgentDir(), "models.json"), []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(coding.GetAgentDir(), "settings.json"), []byte("{ broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := coding.CreateModelRuntime(coding.CreateModelRuntimeOptions{AllowModelNetwork: false})
	if err != nil {
		t.Fatal(err)
	}
	settings := coding.NewSettingsManagerFromFiles(t.TempDir(), coding.GetAgentDir(), coding.SettingsManagerCreateOptions{})
	stdout, stderr := captureStdio(t, func() {
		if err := listModels(context.Background(), runtime, settings, coding.ParseArgs(nil)); err != nil {
			t.Errorf("listModels: %v", err)
		}
	})
	if !strings.Contains(stderr, "errors loading models.json") {
		t.Fatalf("stderr = %q; want the models.json warning", stderr)
	}
	if !strings.Contains(stderr, "settings") {
		t.Fatalf("stderr = %q; want the settings diagnostic", stderr)
	}
	if !strings.Contains(stdout, "No models available.") {
		t.Fatalf("stdout = %q; want the no-models message", stdout)
	}
}

// With a catalog present the table lists the configured provider's model, and
// the --list-models pattern filters rows.
func TestListModelsPrintsTheConfiguredCatalog(t *testing.T) {
	isolatedAgentDir(t)
	agentDir := coding.GetAgentDir()
	models := `{"providers":{"fake":{"baseUrl":"http://127.0.0.1:1/v1","api":"openai-completions","apiKey":"dummy","models":[` +
		`{"id":"fake-1","name":"Fake One","contextWindow":100000,"maxTokens":4096},` +
		`{"id":"other-2","name":"Other Two","contextWindow":2000,"maxTokens":100,"reasoning":true}]}}}`
	if err := os.WriteFile(filepath.Join(agentDir, "models.json"), []byte(models), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := coding.CreateModelRuntime(coding.CreateModelRuntimeOptions{AllowModelNetwork: false})
	if err != nil {
		t.Fatal(err)
	}
	settings := coding.NewSettingsManagerFromFiles(t.TempDir(), agentDir, coding.SettingsManagerCreateOptions{})

	stdout, _ := captureStdio(t, func() {
		if err := listModels(context.Background(), runtime, settings, coding.ParseArgs(nil)); err != nil {
			t.Errorf("listModels: %v", err)
		}
	})
	if !strings.Contains(stdout, "fake-1") || !strings.Contains(stdout, "other-2") {
		t.Fatalf("stdout = %q; want both catalog models", stdout)
	}

	pattern := "other"
	stdout, _ = captureStdio(t, func() {
		if err := listModels(context.Background(), runtime, settings, coding.ParseArgs([]string{"--list-models", pattern})); err != nil {
			t.Errorf("listModels: %v", err)
		}
	})
	if !strings.Contains(stdout, "other-2") || strings.Contains(stdout, "fake-1") {
		t.Fatalf("stdout = %q; want only the matching model", stdout)
	}
}

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

// Offline is process-wide upstream: main.ts sets PI_OFFLINE and
// PI_SKIP_VERSION_CHECK from the flag or the environment before anything reads
// either, and the model runtime and the version check both consult the
// environment rather than the parsed arguments.
func TestApplyOfflineModeSetsTheProcessWideFlags(t *testing.T) {
	cases := []struct {
		name        string
		flag        bool
		env         string
		wantOffline bool
	}{
		{"flag", true, "", true},
		{"env 1", false, "1", true},
		{"env true", false, "true", true},
		{"env yes", false, "yes", true},
		{"env 0", false, "0", false},
		{"env false", false, "false", false},
		{"online", false, "", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("PI_OFFLINE", testCase.env)
			t.Setenv("PI_SKIP_VERSION_CHECK", "")
			applyOfflineMode(&coding.Args{Offline: testCase.flag})
			// The env is not cleared when it is not offline: upstream leaves a
			// falsy flag alone, it only ever sets the two variables.
			if got := coding.IsTruthyEnvFlag(os.Getenv("PI_OFFLINE")); got != testCase.wantOffline {
				t.Errorf("PI_OFFLINE = %q, want offline=%v", os.Getenv("PI_OFFLINE"), testCase.wantOffline)
			}
			if got := os.Getenv("PI_SKIP_VERSION_CHECK") != ""; got != testCase.wantOffline {
				t.Errorf("PI_SKIP_VERSION_CHECK = %q, want set=%v", os.Getenv("PI_SKIP_VERSION_CHECK"), testCase.wantOffline)
			}
		})
	}
}
