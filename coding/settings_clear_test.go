package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSettingsPersistRemovesClearedField pins that a field cleared back to
// unset is removed from the file: upstream assigns undefined, which
// JSON.stringify drops, so the old value must not survive. Removing one of
// several nested entries keeps the others.
func TestSettingsPersistRemovesClearedField(t *testing.T) {
	agentDir := t.TempDir()
	cwd := t.TempDir()
	manager := NewSettingsManagerFromFiles(cwd, agentDir, SettingsManagerCreateOptions{})
	manager.SetModelThinkingLevel("openai", "gpt-5", "high")
	manager.SetModelThinkingLevel("openai", "gpt-4o", "low")
	manager.FlushPersists()

	read := func() map[string]json.RawMessage {
		data, err := os.ReadFile(filepath.Join(agentDir, "settings.json"))
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			t.Fatal(err)
		}
		return object
	}
	if read()["modelThinkingLevels"] == nil {
		t.Fatal("the overrides were not persisted")
	}

	// Removing one entry keeps the other.
	manager.RemoveModelThinkingLevel("openai", "gpt-5")
	manager.FlushPersists()
	var remaining map[string]string
	if err := json.Unmarshal(read()["modelThinkingLevels"], &remaining); err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining["openai/gpt-4o"] != "low" {
		t.Fatalf("remaining = %v", remaining)
	}

	// Removing the last entry clears the field entirely.
	manager.RemoveModelThinkingLevel("openai", "gpt-4o")
	manager.FlushPersists()
	if raw := read()["modelThinkingLevels"]; raw != nil {
		t.Fatalf("a cleared field survived: %s", raw)
	}
}
