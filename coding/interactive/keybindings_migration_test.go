package interactive

import (
	"os"
	"path/filepath"
	"testing"
)

// Upstream migrations.ts migrateKeybindingsConfigFile rewrites a legacy
// keybindings.json to namespaced ids when the startup migration runs, writing
// two-space-indented JSON with a trailing newline.
func TestCreateAppKeybindingsMigratesLegacyFile(t *testing.T) {
	agentDir := t.TempDir()
	path := filepath.Join(agentDir, "keybindings.json")
	legacy := "{\n  \"cursorUp\": [\n    \"up\",\n    \"ctrl+p\"\n  ],\n  \"expandTools\": \"ctrl+x\"\n}\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := CreateAppKeybindings(agentDir)
	if got := manager.GetUserBindings()["tui.editor.cursorUp"]; len(got) != 2 || got[0] != "up" || got[1] != "ctrl+p" {
		t.Fatalf("in-memory bindings = %v", manager.GetUserBindings())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"tui.editor.cursorUp\": [\n    \"up\",\n    \"ctrl+p\"\n  ],\n  \"app.tools.expand\": \"ctrl+x\"\n}\n"
	if string(data) != want {
		t.Fatalf("migrated keybindings.json = %q; want %q", data, want)
	}
}

// The migration keeps the namespaced value when both names exist and leaves a
// non-legacy file untouched.
func TestCreateAppKeybindingsMigrationKeepsModernValues(t *testing.T) {
	agentDir := t.TempDir()
	path := filepath.Join(agentDir, "keybindings.json")
	both := "{\n  \"expandTools\": \"ctrl+x\",\n  \"app.tools.expand\": \"ctrl+y\"\n}\n"
	if err := os.WriteFile(path, []byte(both), 0o644); err != nil {
		t.Fatal(err)
	}
	CreateAppKeybindings(agentDir)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"app.tools.expand\": \"ctrl+y\"\n}\n"; string(data) != want {
		t.Fatalf("migrated keybindings.json = %q; want %q", data, want)
	}

	modern := "{\n  \"app.tools.expand\": [\n    \"ctrl+y\"\n  ]\n}"
	if err := os.WriteFile(path, []byte(modern), 0o644); err != nil {
		t.Fatal(err)
	}
	CreateAppKeybindings(agentDir)
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != modern {
		t.Fatalf("modern keybindings.json rewritten: %q", data)
	}
}

// Malformed files are ignored, like upstream's try/catch around the migration.
func TestCreateAppKeybindingsMigrationIgnoresMalformedFile(t *testing.T) {
	agentDir := t.TempDir()
	path := filepath.Join(agentDir, "keybindings.json")
	malformed := "{not json"
	if err := os.WriteFile(path, []byte(malformed), 0o644); err != nil {
		t.Fatal(err)
	}
	CreateAppKeybindings(agentDir)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != malformed {
		t.Fatalf("malformed keybindings.json rewritten: %q", data)
	}
}
