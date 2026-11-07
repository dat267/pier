package coding

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.5.0 settings-manager.ts persistScopedSettings,
// extensions/mcp/config.ts editMcpServers and models-store.ts read/parse all
// route through JSON.parse + JSON.stringify, so top-level array-index keys
// (`"1"`, `"2"`, `"10"`, but not `"01"`, negative or 2^32-1) enumerate first in
// numeric order before the ordinary keys keep file order. Pier kept pure file
// order.
func TestPersistedJSONWritersUseJavaScriptKeyOrder(t *testing.T) {
	const numericFile = "{\n  \"10\": 1,\n  \"2\": 2,\n  \"b\": 3,\n  \"1\": 4\n}\n"
	const numericWanted = "{\n  \"1\": 4,\n  \"2\": 2,\n  \"10\": 1,\n  \"b\": 3,\n  \"theme\": \"dark\"\n}"

	settingsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(numericFile), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewSettingsManagerFromFiles(t.TempDir(), settingsDir, SettingsManagerCreateOptions{})
	manager.SetTheme("dark")
	manager.FlushPersists()
	settings, err := os.ReadFile(filepath.Join(settingsDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(settings) != numericWanted {
		t.Errorf("settings bytes:\ngot  %q\nwant %q", settings, numericWanted)
	}

	mcpPath := filepath.Join(t.TempDir(), "mcp.json")
	mcpFile := "{\n  \"10\": {\"x\": 1},\n  \"2\": {},\n  \"b\": {},\n  \"1\": {}\n}\n"
	if err := os.WriteFile(mcpPath, []byte(mcpFile), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AddMcpServerConfig(mcpPath, "s", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	mcp, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	mcpWanted := "{\n  \"1\": {},\n  \"2\": {},\n  \"10\": {\n    \"x\": 1\n  },\n  \"b\": {},\n  \"mcpServers\": {\n    \"s\": {}\n  }\n}\n"
	if string(mcp) != mcpWanted {
		t.Errorf("mcp bytes:\ngot  %q\nwant %q", mcp, mcpWanted)
	}

	modelsPath := filepath.Join(t.TempDir(), "models-store.json")
	modelsFile := "{\n  \"10\": {\"models\": []},\n  \"2\": {\"models\": []},\n  \"b\": {\"models\": []},\n  \"1\": {\"models\": []}\n}"
	if err := os.WriteFile(modelsPath, []byte(modelsFile), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewFileModelsStore(modelsPath)
	if err := store.Write("prov", &ai.ModelsStoreEntry{}, context.Background()); err != nil {
		t.Fatal(err)
	}
	models, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatal(err)
	}
	modelsWanted := "{\n  \"1\": {\n    \"models\": []\n  },\n  \"2\": {\n    \"models\": []\n  },\n  \"10\": {\n    \"models\": []\n  },\n  \"b\": {\n    \"models\": []\n  },\n  \"prov\": {\n    \"models\": []\n  }\n}"
	if string(models) != modelsWanted {
		t.Errorf("models-store bytes:\ngot  %q\nwant %q", models, modelsWanted)
	}
}
