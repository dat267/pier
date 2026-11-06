package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
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
		if err := listModels(runtime, settings, coding.ParseArgs(nil), context.Background()); err != nil {
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
		if err := listModels(runtime, settings, coding.ParseArgs(nil), context.Background()); err != nil {
			t.Errorf("listModels: %v", err)
		}
	})
	if !strings.Contains(stdout, "fake-1") || !strings.Contains(stdout, "other-2") {
		t.Fatalf("stdout = %q; want both catalog models", stdout)
	}

	pattern := "other"
	stdout, _ = captureStdio(t, func() {
		if err := listModels(runtime, settings, coding.ParseArgs([]string{"--list-models", pattern}), context.Background()); err != nil {
			t.Errorf("listModels: %v", err)
		}
	})
	if !strings.Contains(stdout, "other-2") || strings.Contains(stdout, "fake-1") {
		t.Fatalf("stdout = %q; want only the matching model", stdout)
	}
}
