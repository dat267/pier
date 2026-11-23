package coding

import (
	ctxpkg "context"
	"path/filepath"
	"testing"
)

// The stored goal-feature setting decides whether a new session installs the
// controller (D496), the same way /settings applies it to a live one.
func TestCreateAgentSessionHonoursTheGoalFeatureSetting(t *testing.T) {
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
	settings.SetGoalFeatureEnabled(false)
	create := func() *AgentSession {
		result, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
			Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
		})
		if err != nil {
			t.Fatal(err)
		}
		return result.Session
	}
	disabled := create()
	if disabled.GoalController() != nil {
		t.Fatal("goal controller installed with the setting off")
	}
	for _, name := range disabled.GetActiveToolNames() {
		if name == "set_goal" || name == "get_goal" {
			t.Fatalf("goal tool active with the setting off: %v", disabled.GetActiveToolNames())
		}
	}
	disabled.Dispose()

	settings.SetGoalFeatureEnabled(true)
	enabled := create()
	defer enabled.Dispose()
	if enabled.GoalController() == nil {
		t.Fatal("no goal controller with the setting on")
	}
	found := 0
	for _, name := range enabled.GetActiveToolNames() {
		if name == "set_goal" || name == "get_goal" {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("goal tools = %d of 2 with the setting on: %v", found, enabled.GetActiveToolNames())
	}
}
