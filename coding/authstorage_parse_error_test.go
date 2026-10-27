package coding

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), auth-storage.ts reload catches parseStorageData's
// JSON.parse error and retains the last valid read snapshot.
func TestAuthStorageReloadPreservesSnapshotOnTruncatedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	writeAuthFile(t, path, `{"p":{"type":"api_key","key":"synthetic-original"}}`)
	storage := NewAuthStorage(path)
	writeAuthFile(t, path, `{"p":{"type":"api_key","key":"synthetic-corrupt"}`)
	storage.Reload()
	credential, err := storage.Read("p", context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if credential == nil || credential.APIKey == nil || credential.APIKey.Key != "synthetic-original" {
		t.Error("reload replaced the valid credential snapshot with a truncated document")
	}
}

// The same JSON.parse boundary precedes delete; a parsed object prefix is not
// enough when trailing bytes make the full auth.json invalid.
func TestAuthStorageDeleteDoesNotOverwriteMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	writeAuthFile(t, path, `{"p":{"type":"api_key","key":"synthetic-original"}}`)
	storage := NewAuthStorage(path)
	const malformed = `{"p":{"type":"api_key","key":"synthetic-original"}} trailing`
	writeAuthFile(t, path, malformed)
	if err := storage.Delete("p", context.Background()); err == nil {
		t.Error("delete acknowledged malformed auth.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != malformed {
		t.Error("delete overwrote malformed auth.json")
	}
}

// pi v1.4.0 (4a080252), core/auth-storage.ts parseStorageData uses JSON.parse;
// a malformed file rejects modify before invoking its callback or writing.
func TestAuthStorageModifyDoesNotOverwriteMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	writeAuthFile(t, path, `{"p":{"type":"api_key","key":"synthetic-original"}}`)
	storage := NewAuthStorage(path)
	const malformed = `{"p":{"type":"api_key","key":"synthetic-original"},"other":`
	writeAuthFile(t, path, malformed)
	called := false
	_, err := storage.Modify("other", func(*ai.Credential) (*ai.Credential, error) {
		called = true
		return &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "synthetic-new"}}, nil
	}, context.Background())
	if err == nil {
		t.Error("modify acknowledged malformed auth.json as a valid empty store")
	}
	if called {
		t.Error("modify callback ran despite malformed auth.json")
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(raw) != malformed {
		t.Error("modify overwrote malformed auth.json instead of preserving it for repair")
	}
}
