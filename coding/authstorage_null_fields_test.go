package coding

import (
	"context"
	"path/filepath"
	"testing"
)

// Reference: pi v1.5.0 f9a26365 core/auth-storage.ts ReadOnlyAuthStorage.load.
// Missing key/env fields are allowed, but present null values are not strings
// or environment objects and must fail a no-refresh auth-state check.
func TestReadOnlyAuthStorageRejectsExplicitNullFields(t *testing.T) {
	for _, tc := range []struct {
		name, credential string
		valid            bool
	}{
		{"null key", `{"type":"api_key","key":null}`, false},
		{"null env", `{"type":"api_key","key":"synthetic","env":null}`, false},
		{"missing key", `{"type":"api_key"}`, true},
		{"missing env", `{"type":"api_key","key":"synthetic"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.json")
			writeAuthFile(t, path, `{"p":`+tc.credential+`}`)
			_, err := NewReadOnlyAuthStorage(path).List(context.Background())
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != `Invalid auth.json credential for provider "p"` {
				t.Fatalf("invalid credential error = %v", err)
			}
		})
	}
}
