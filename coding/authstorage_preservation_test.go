package coding

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/auth-storage.ts modify spreads the parsed
// document and delete removes only its target. Untouched entries and unknown
// credential members survive, even when our typed view cannot decode them.
func TestAuthStorageMutationsPreserveUntouchedEntries(t *testing.T) {
	data, err := os.ReadFile("testdata/auth-preservation/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Name, Initial, Op, Expected string }
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name+"/"+tc.Op, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.json")
			writeAuthFile(t, path, tc.Initial)
			storage := NewAuthStorage(path)
			known, err := storage.Read("known", context.Background())
			if err != nil || known == nil || known.APIKey == nil || known.APIKey.Key != "synthetic-known" {
				t.Fatalf("readable sibling was lost: %+v, %v", known, err)
			}
			if tc.Op == "modify" {
				_, err = storage.Modify("added", func(*ai.Credential) (*ai.Credential, error) {
					return &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "synthetic-added"}}, nil
				}, context.Background())
			} else {
				err = storage.Delete("known", context.Background())
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.Expected {
				t.Fatalf("auth.json = %s; want %s", got, tc.Expected)
			}
		})
	}
}
