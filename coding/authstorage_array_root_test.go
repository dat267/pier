package coding

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/auth-storage.ts spreads the parsed document
// during modify. For a valid JSON array this retains its enumerable entries
// ("0", "1", ...) instead of silently replacing the whole document.
func TestAuthStorageArrayRootIsNotDiscardedByMutation(t *testing.T) {
	const initial = `["preserve-me"]`
	path := filepath.Join(t.TempDir(), "auth.json")
	writeAuthFile(t, path, initial)
	storage := NewAuthStorage(path)
	_, err := storage.Modify("openai", func(*ai.Credential) (*ai.Credential, error) {
		return &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "synthetic-key"}}, nil
	}, context.Background())
	if err != nil {
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != initial {
			t.Fatalf("rejected mutation changed auth.json: %s", got)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(got, &document); err != nil {
		t.Fatalf("mutated auth.json is invalid: %v", err)
	}
	var preserved string
	if err := json.Unmarshal(document["0"], &preserved); err != nil || preserved != "preserve-me" {
		t.Fatalf("array entry was lost: document=%s entry=%q err=%v", got, preserved, err)
	}
	if len(document["openai"]) == 0 {
		t.Fatalf("new credential was not written: %s", got)
	}
}
