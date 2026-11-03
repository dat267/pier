package coding

import (
	"context"
	"path/filepath"
	"testing"
)

// Reference: pi v1.5.0 f9a26365 core/auth-storage.ts ReadOnlyAuthStorage.read
// uses structuredClone. Returned environment maps and OAuth extra JSON belong
// to the caller, not to the cached file snapshot.
func TestReadOnlyAuthStorageReturnsOwnedCredentialData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	writeAuthFile(t, path, `{"api":{"type":"api_key","key":"synthetic","env":{"ACCOUNT":"original"}},"oauth":{"type":"oauth","access":"synthetic","refresh":"synthetic","expires":123,"tenant":"original"}}`)
	storage := NewReadOnlyAuthStorage(path)
	ctx := context.Background()
	api, err := storage.Read("api", ctx)
	if err != nil {
		t.Fatal(err)
	}
	api.APIKey.Env["ACCOUNT"] = "changed"
	again, err := storage.Read("api", ctx)
	if err != nil || again.APIKey.Env["ACCOUNT"] != "original" {
		t.Errorf("environment mutation reached cached credential: %v, %v", again.APIKey.Env, err)
	}
	oauth, err := storage.Read("oauth", ctx)
	if err != nil {
		t.Fatal(err)
	}
	oauth.OAuth.Extra["tenant"][1] = 'X'
	oauth.OAuth.Extra["added"] = []byte(`true`)
	again, err = storage.Read("oauth", ctx)
	if err != nil || string(again.OAuth.Extra["tenant"]) != `"original"` || again.OAuth.Extra["added"] != nil {
		t.Errorf("OAuth mutation reached cached credential: %v, %v", again.OAuth.Extra, err)
	}
}
