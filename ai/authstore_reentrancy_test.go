package ai

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// pi v1.4.0 (4a080252), auth/credential-store.ts runs modify callbacks
// outside its state map operations; read does not wait on the write chain.
func TestInMemoryCredentialStoreModifyCanRead(t *testing.T) {
	store := NewInMemoryCredentialStore()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := store.Modify("p", func(current *Credential) (*Credential, error) {
			_, err := store.Read("p", ctx)
			return current, err
		}, ctx)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}

	case <-ctx.Done():
		t.Fatal("modify callback deadlocked reading its store")
	}
}

// D272: Go callbacks edit owned snapshots so concurrent/reentrant readers see
// committed credentials, unlike pi v1.4.0's single-threaded live references.
func TestInMemoryCredentialStoreCallbackUsesCommittedSnapshot(t *testing.T) {
	for _, outcome := range []string{"commit", "error", "cancel"} {
		t.Run(outcome, func(t *testing.T) {
			store := NewInMemoryCredentialStore()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err := store.Modify("p", func(*Credential) (*Credential, error) {
				return &Credential{Type: CredentialAPIKey, APIKey: &ApiKeyCredential{Key: "old", Env: ProviderEnv{"scope": "old"}}}, nil
			}, ctx)
			if err != nil {
				t.Fatal(err)
			}
			callbackErr := errors.New("callback failed")
			_, err = store.Modify("p", func(current *Credential) (*Credential, error) {
				current.APIKey.Key = "new"
				current.APIKey.Env["scope"] = "new"
				committed, readErr := store.Read("p", ctx)
				if readErr != nil {
					return nil, readErr
				}
				if committed.APIKey.Key != "old" || committed.APIKey.Env["scope"] != "old" {
					t.Error("callback edits leaked before commit")
				}
				switch outcome {
				case "error":
					return nil, callbackErr
				case "cancel":
					cancel()
				}
				return nil, nil // A successful in-place edit is still a credential update.
			}, ctx)
			want := "old"
			switch outcome {
			case "commit":
				want = "new"
				if err != nil {
					t.Fatal(err)
				}
			case "error":
				if !errors.Is(err, callbackErr) {
					t.Fatalf("error = %v, want callback error", err)
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v, want cancellation", err)
				}
			}
			saved, readErr := store.Read("p", context.Background())
			if readErr != nil {
				t.Fatal(readErr)
			}

			if saved.APIKey.Key != want || saved.APIKey.Env["scope"] != want {
				t.Errorf("saved credential = %+v, want %s", saved.APIKey, want)
			}
		})
	}
}

// D272 also covers OAuth extras, whose RawMessages must not alias the store.
func TestInMemoryCredentialStoreOAuthCallbackUsesSnapshot(t *testing.T) {
	store := NewInMemoryCredentialStore()
	ctx := context.Background()
	original := &Credential{Type: CredentialOAuth, OAuth: &OAuthCredential{OAuthCredentials{Access: "old", Extra: map[string]json.RawMessage{"note": json.RawMessage(`"old"`)}}}}
	_, err := store.Modify("p", func(*Credential) (*Credential, error) { return original, nil }, ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Modify("p", func(current *Credential) (*Credential, error) {
		current.OAuth.Access = "new"
		current.OAuth.Extra["note"][1] = 'n'
		saved, err := store.Read("p", ctx)
		if err != nil {
			return nil, err
		}
		if saved.OAuth.Access != "old" || string(saved.OAuth.Extra["note"]) != `"old"` {
			t.Error("OAuth edits leaked before commit")
		}
		return nil, nil
	}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Read("p", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if saved.OAuth.Access != "new" || string(saved.OAuth.Extra["note"]) != `"nld"` {
		t.Errorf("OAuth edits not committed: %+v", saved.OAuth)
	}
	// Neither a returned snapshot nor the caller's original credential owns state.
	saved.OAuth.Access = "external"
	original.OAuth.Extra["note"][1] = 'x'
	stored, err := store.Read("p", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stored.OAuth.Access != "new" || string(stored.OAuth.Extra["note"]) != `"nld"` {
		t.Error("external OAuth alias mutated the store")
	}
}
