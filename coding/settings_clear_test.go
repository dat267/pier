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

// TestSettingsPersistRemovesClearedNestedField pins the nested branch: clearing
// a nested field removes it from the object, and its siblings survive.
func TestSettingsPersistRemovesClearedNestedField(t *testing.T) {
	agentDir := t.TempDir()
	cwd := t.TempDir()
	manager := NewSettingsManagerFromFiles(cwd, agentDir, SettingsManagerCreateOptions{})
	manager.SetCompactionEnabled(true)
	manager.mu.Lock()
	reserve := int64(1234)
	manager.globalSettings.Compaction.ReserveTokens = &reserve
	manager.markModified("compaction", "reserveTokens")
	manager.save()
	manager.mu.Unlock()
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
	var compaction map[string]json.RawMessage
	if err := json.Unmarshal(read()["compaction"], &compaction); err != nil {
		t.Fatal(err)
	}
	if compaction["enabled"] == nil || compaction["reserveTokens"] == nil {
		t.Fatalf("setup failed: %v", compaction)
	}

	// Clear `enabled`: it must be removed, `reserveTokens` kept.
	manager.mu.Lock()
	manager.globalSettings.Compaction.Enabled = nil
	manager.markModified("compaction", "enabled")
	manager.save()
	manager.mu.Unlock()
	manager.FlushPersists()
	var after map[string]json.RawMessage
	if err := json.Unmarshal(read()["compaction"], &after); err != nil {
		t.Fatal(err)
	}
	if after["enabled"] != nil {
		t.Fatalf("a cleared nested field survived: %s", after["enabled"])
	}
	if after["reserveTokens"] == nil {
		t.Fatal("a sibling nested field was dropped")
	}
}
