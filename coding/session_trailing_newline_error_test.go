package coding

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Reference: pi v1.5.2 b4afdddf core/session-manager.ts loadEntriesFromFile
// calls appendFileSync when the final JSONL entry has no newline; a failed
// repair must fail the open before a later append can concatenate entries.
func TestOpenSessionReportsFailedTrailingNewlineRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := "{\"type\":\"session\",\"version\":3,\"id\":\"s\",\"timestamp\":\"2026-01-01T00:00:00.000Z\",\"cwd\":\"/tmp\"}\n" +
		"{\"type\":\"message\",\"id\":\"m\",\"parentId\":null,\"timestamp\":\"2026-01-01T00:00:01.000Z\",\"message\":{\"role\":\"user\",\"content\":\"hello\",\"timestamp\":1}}"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("simulated newline repair failure")
	original := sessionLineWriter
	sessionLineWriter = func(gotPath, line string) error {
		if gotPath == path && line == "\n" {
			return wantErr
		}
		return original(gotPath, line)
	}
	t.Cleanup(func() { sessionLineWriter = original })

	if _, err := OpenSession(path, "", ""); !errors.Is(err, wantErr) {
		t.Fatalf("OpenSession error = %v, want the failed newline repair", err)
	}
}

func TestLoadEntriesFromFileReportsFailedTrailingNewlineRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := "{\"type\":\"session\",\"version\":3,\"id\":\"s\",\"timestamp\":\"2026-01-01T00:00:00.000Z\",\"cwd\":\"/tmp\"}\n" +
		"{\"type\":\"message\",\"id\":\"m\",\"parentId\":null,\"timestamp\":\"2026-01-01T00:00:01.000Z\",\"message\":{\"role\":\"user\",\"content\":\"hello\",\"timestamp\":1}}"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("simulated newline repair failure")
	original := sessionLineWriter
	sessionLineWriter = func(gotPath, line string) error {
		if gotPath == path && line == "\n" {
			return wantErr
		}
		return original(gotPath, line)
	}
	t.Cleanup(func() { sessionLineWriter = original })

	if _, err := LoadEntriesFromFile(path); !errors.Is(err, wantErr) {
		t.Fatalf("LoadEntriesFromFile error = %v, want the failed newline repair", err)
	}
}
