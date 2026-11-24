package coding

import (
	ctxpkg "context"
	"path/filepath"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reopening a session whose stored model is gone warns on every open: the
// restore only formats a fallback message and never replaces the durable
// model_change entry, so the next open sees the same missing model
// (core/sdk.ts:222-250 in the pin: `getBranchSelection` reads the branch and
// only `modelFallbackMessage` is built; nothing appends a change).
func TestReopeningASessionWithAMissingModelWarnsAgain(t *testing.T) {
	tempAgentDir(t)
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		AuthPath:        filepath.Join(GetAgentDir(), "auth.json"),
		ModelsPath:      filepath.Join(GetAgentDir(), "models.json"),
		Credentials:     newMemoryCredentialStore(),
		RefreshOnCreate: new(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetRuntimeAPIKey(ctxpkg.Background(), "anthropic", "sk-1"); err != nil {
		t.Fatal(err)
	}
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	settings.SetDefaultProvider("anthropic")

	manager := newTestSessionManager(t)
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}})
	manager.AppendModelChange("anthropic", "a-model-that-left-the-catalog")

	countModelChanges := func() int {
		count := 0
		for _, entry := range manager.GetBranch("") {
			if entry.Type == "model_change" {
				count++
			}
		}
		return count
	}
	open := func() *CreateAgentSessionResult {
		result, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
			Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings, SessionManager: manager,
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := countModelChanges()
	first := open()
	if first.ModelFallbackMessage == "" {
		t.Fatal("first open did not report the missing model")
	}
	if after := countModelChanges(); after != before {
		t.Fatalf("model_change entries = %d, want %d: the fallback must not be persisted", after, before)
	}
	second := open()
	if second.ModelFallbackMessage == "" {
		t.Fatal("second open did not warn again")
	}
	if second.ModelFallbackMessage != first.ModelFallbackMessage {
		t.Fatalf("second warning = %q, first = %q", second.ModelFallbackMessage, first.ModelFallbackMessage)
	}
	if after := countModelChanges(); after != before {
		t.Fatalf("model_change entries = %d after two opens, want %d", after, before)
	}
}
