package coding

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// Upstream core/sdk.ts assembles the built-in tools without a filesystem
// sandbox. Confinement belongs to extensions, not the session system prompt.
func TestCreateAgentSessionDoesNotInjectSandboxPolicy(t *testing.T) {
	tempAgentDir(t)
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		Credentials: newMemoryCredentialStore(), RefreshOnCreate: boolPtr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	result, err := CreateAgentSession(context.Background(), &CreateAgentSessionOptions{
		Cwd: cwd, ModelRuntime: runtime,
		SessionManager:  NewSessionManager(cwd, nil),
		SettingsManager: NewSettingsManagerFromFiles(cwd, filepath.Join(t.TempDir(), "agent"), SettingsManagerCreateOptions{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	prompt := result.Session.SystemPrompt()
	if strings.Contains(prompt, "<sandbox>") {
		t.Fatal("built-in session injected a sandbox policy absent from pi")
	}
}
