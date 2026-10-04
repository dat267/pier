package coding

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGetTerminalCapabilityOverrides pins the terminal.trueColor/hyperlinks/
// images overrides (upstream getTerminalCapabilityOverrides).
func TestGetTerminalCapabilityOverrides(t *testing.T) {
	agentDir := t.TempDir()
	cwd := t.TempDir()
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(agentDir, "settings.json"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Absent: no override.
	write(`{}`)
	overrides := NewSettingsManagerFromFiles(cwd, agentDir, SettingsManagerCreateOptions{}).GetTerminalCapabilityOverrides()
	if overrides.TrueColor != nil || overrides.Hyperlinks != nil || overrides.HasImages {
		t.Fatalf("absent = %+v", overrides)
	}

	// Boolean overrides and a kitty image mode.
	write(`{"terminal":{"trueColor":false,"hyperlinks":true,"images":"kitty"}}`)
	overrides = NewSettingsManagerFromFiles(cwd, agentDir, SettingsManagerCreateOptions{}).GetTerminalCapabilityOverrides()
	if overrides.TrueColor == nil || *overrides.TrueColor {
		t.Fatalf("trueColor = %v", overrides.TrueColor)
	}
	if overrides.Hyperlinks == nil || !*overrides.Hyperlinks {
		t.Fatalf("hyperlinks = %v", overrides.Hyperlinks)
	}
	if !overrides.HasImages || overrides.Images != "kitty" {
		t.Fatalf("images = %v (%v)", overrides.Images, overrides.HasImages)
	}

	// `"auto"`/absent values are not overrides; `images: false` is.
	write(`{"terminal":{"trueColor":"auto","hyperlinks":"auto","images":false}}`)
	overrides = NewSettingsManagerFromFiles(cwd, agentDir, SettingsManagerCreateOptions{}).GetTerminalCapabilityOverrides()
	if overrides.TrueColor != nil || overrides.Hyperlinks != nil {
		t.Fatalf("auto values became overrides: %+v", overrides)
	}
	if !overrides.HasImages || overrides.Images != nil {
		t.Fatalf("images false = %v (%v)", overrides.Images, overrides.HasImages)
	}
}
