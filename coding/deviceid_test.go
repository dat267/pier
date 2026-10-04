package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestGetOrCreateDeviceID covers the installation UUID: created on first use,
// persisted to global settings, stable across managers.
func TestGetOrCreateDeviceID(t *testing.T) {
	agentDir := t.TempDir()
	cwd := t.TempDir()
	manager := NewSettingsManagerFromFiles(cwd, agentDir, SettingsManagerCreateOptions{})

	first := manager.GetOrCreateDeviceID()
	if !isUUIDString(first) {
		t.Fatalf("device id = %q", first)
	}
	if again := manager.GetOrCreateDeviceID(); again != first {
		t.Fatalf("device id changed: %q vs %q", again, first)
	}
	manager.FlushPersists()

	// The id lands in the global settings file and is stable across managers.
	data, err := os.ReadFile(filepath.Join(agentDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := json.Unmarshal(probe["deviceId"], &stored); err != nil || stored != first {
		t.Fatalf("stored deviceId = %s, %v", probe["deviceId"], err)
	}
	if reloaded := NewSettingsManagerFromFiles(cwd, agentDir, SettingsManagerCreateOptions{}).GetOrCreateDeviceID(); reloaded != first {
		t.Fatalf("reloaded device id = %q, want %q", reloaded, first)
	}
}

func isUUIDString(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		switch index {
		case 8, 13, 18, 23:
			if char != '-' {
				return false
			}
		default:
			if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
				return false
			}
		}
	}
	return true
}
