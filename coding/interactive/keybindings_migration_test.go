package interactive

import (
	"encoding/json"
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

// Pinned keybindings.ts orders migrated keys by KEYBINDINGS declaration order,
// not alphabetical order.
func TestCreateAppKeybindingsMigrationUsesReferenceOrder(t *testing.T) {
	agentDir := t.TempDir()
	path := filepath.Join(agentDir, "keybindings.json")
	if err := os.WriteFile(path, []byte(`{"cursorUp":"ctrl+u","cursorDown":"ctrl+d"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	CreateAppKeybindings(agentDir)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"tui.editor.cursorUp\": \"ctrl+u\",\n  \"tui.editor.cursorDown\": \"ctrl+d\"\n}\n"
	if string(data) != want {
		t.Fatalf("migrated keybindings.json = %q; want %q", data, want)
	}
}

// Pinned key-id-schema.ts rejects values outside the KeyId pattern.
func TestAppKeybindingsIgnoreInvalidUserKeyIDs(t *testing.T) {
	agentDir := t.TempDir()
	path := filepath.Join(agentDir, "keybindings.json")
	if err := os.WriteFile(path, []byte(`{"app.clear":"bogus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := CreateAppKeybindings(agentDir)
	if got := manager.GetKeys("app.clear"); len(got) != 1 || got[0] != "ctrl+c" {
		t.Fatalf("invalid user keybinding resolved to %v; want default ctrl+c", got)
	}
}

func TestAppKeybindingsRejectInvalidMemberFromUserKeyIDArray(t *testing.T) {
	agentDir := t.TempDir()
	path := filepath.Join(agentDir, "keybindings.json")
	if err := os.WriteFile(path, []byte(`{"app.clear":["ctrl+x","bogus"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := CreateAppKeybindings(agentDir)
	if got := manager.GetKeys("app.clear"); len(got) != 1 || got[0] != "ctrl+c" {
		t.Fatalf("invalid user keybinding array resolved to %v; want default ctrl+c", got)
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

// marshalOrderedKeybindings renders the migrated file as JSON.stringify(config, null,
// 2) plus a trailing newline. A keybinding value is whatever the user wrote, so it can
// be an object or an array, and JSON.stringify re-indents EVERY level: a nested
// container is never emitted on one line.
func TestMarshalKeybindingsIndentsNestedContainers(t *testing.T) {
	config := map[string]json.RawMessage{
		"app.interrupt": json.RawMessage(`{"keys":["escape","ctrl+c"]}`),
		"app.quit":      json.RawMessage(`["q"]`),
		"app.submit":    json.RawMessage(`"enter"`),
	}
	out, err := marshalOrderedKeybindings(config)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n" +
		"  \"app.interrupt\": {\n" +
		"    \"keys\": [\n" +
		"      \"escape\",\n" +
		"      \"ctrl+c\"\n" +
		"    ]\n" +
		"  },\n" +
		"  \"app.quit\": [\n" +
		"    \"q\"\n" +
		"  ],\n" +
		"  \"app.submit\": \"enter\"\n" +
		"}\n"
	if string(out) != want {
		t.Fatalf("emitted\n%q\nwant\n%q", string(out), want)
	}
}
