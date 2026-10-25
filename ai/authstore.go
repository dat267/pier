package ai

import (
	"context"
	"maps"
	"slices"
	"sync"
)

// Port of auth/credential-store.ts (InMemoryCredentialStore) and
// models-store.ts (InMemoryModelsStore).

// InMemoryCredentialStore is the default in-memory credential store. Apps
// inject persistent stores. Writes are serialized per provider id (upstream
// serializes through a promise chain; Go uses completion-channel chains).
// The data mutex is never retained across a user callback.
type InMemoryCredentialStore struct {
	mu    sync.Mutex
	creds map[string]*Credential
	tails map[string]chan struct{}
}

func NewInMemoryCredentialStore() *InMemoryCredentialStore {
	return &InMemoryCredentialStore{creds: map[string]*Credential{}, tails: map[string]chan struct{}{}}
}

func (s *InMemoryCredentialStore) Read(providerID string, ctx context.Context) (*Credential, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneMemoryCredential(s.creds[providerID]), nil
}

func (s *InMemoryCredentialStore) List(ctx context.Context) ([]CredentialInfo, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CredentialInfo, 0, len(s.creds))
	for id, c := range s.creds {
		out = append(out, CredentialInfo{ProviderID: id, Type: c.Type})
	}
	return out, nil
}

func (s *InMemoryCredentialStore) beginWrite(providerID string, ctx context.Context) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	previous := s.tails[providerID]
	done := make(chan struct{})
	s.tails[providerID] = done
	s.mu.Unlock()
	finish := func() {
		s.mu.Lock()
		if s.tails[providerID] == done {
			delete(s.tails, providerID)
		}
		close(done)
		s.mu.Unlock()
	}
	if previous != nil {
		select {
		case <-previous:
		case <-ctx.Done():
			// Keep this cancelled tail behind the active operation: releasing
			// it now would admit later writes before that operation settles.
			go func() { <-previous; finish() }()
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		finish()
		return nil, err
	}
	return finish, nil
}

func (s *InMemoryCredentialStore) Modify(providerID string, fn func(current *Credential) (*Credential, error), ctx context.Context) (*Credential, error) {
	finish, err := s.beginWrite(providerID, ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	s.mu.Lock()
	current := cloneMemoryCredential(s.creds[providerID])
	s.mu.Unlock()
	next, err := fn(current)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if next == nil {
		next = current
	}
	if next != nil {
		s.creds[providerID] = cloneMemoryCredential(next)
	}
	return cloneMemoryCredential(next), nil
}

// D272: owned snapshots let callbacks run outside the data mutex without
// exposing in-flight edits or caller-owned maps to concurrent readers.
func cloneMemoryCredential(credential *Credential) *Credential {
	if credential == nil {
		return nil
	}
	cloned := *credential
	if credential.APIKey != nil {
		key := *credential.APIKey
		key.Env = maps.Clone(key.Env)
		cloned.APIKey = &key
	}
	if credential.OAuth != nil {
		oauth := *credential.OAuth
		oauth.Extra = maps.Clone(oauth.Extra)
		for key, raw := range oauth.Extra {
			oauth.Extra[key] = slices.Clone(raw)
		}
		cloned.OAuth = &oauth
	}
	return &cloned
}

func (s *InMemoryCredentialStore) Delete(providerID string, ctx context.Context) error {
	finish, err := s.beginWrite(providerID, ctx)
	if err != nil {
		return err
	}
	defer finish()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctxErr(ctx); err != nil {
		return err
	}
	delete(s.creds, providerID)
	return nil
}

// ModelsStoreEntry is one persisted model catalog keyed by provider ID.
type ModelsStoreEntry struct {
	Models []*Model `json:"models"`
	// LastModified is the Unix timestamp from the remote catalog's
	// Last-Modified header.
	LastModified *int64 `json:"lastModified,omitempty"`
	// CheckedAt is the Unix timestamp of the last completed remote check.
	CheckedAt *int64 `json:"checkedAt,omitempty"`
	// Etag is an opaque validator from the remote catalog's ETag header,
	// stored verbatim (quotes included) and echoed back as If-None-Match.
	Etag *string `json:"etag,omitempty"`
}

// MarshalJSON preserves pi's required array shape for an empty catalog:
// models-store.ts declares models as readonly AnyModel[], never null. D271:
// upstream's FileModelsStore.write round-trips the parsed file, so a legacy
// "models":null left by an older Pier build would survive a write; this
// normalizes every entry to the array shape pi expects.
func (entry ModelsStoreEntry) MarshalJSON() ([]byte, error) {
	type plain ModelsStoreEntry
	if entry.Models == nil {
		entry.Models = []*Model{}
	}
	return MarshalJSON(plain(entry))
}

// ModelsStore is persistent model catalogs keyed by provider ID.
type ModelsStore interface {
	Read(providerID string, ctx context.Context) (*ModelsStoreEntry, error)
	Write(providerID string, entry *ModelsStoreEntry, ctx context.Context) error
	Delete(providerID string, ctx context.Context) error
}

// InMemoryModelsStore is the default in-memory models store.
type InMemoryModelsStore struct {
	mu      sync.Mutex
	entries map[string]*ModelsStoreEntry
}

func NewInMemoryModelsStore() *InMemoryModelsStore {
	return &InMemoryModelsStore{entries: map[string]*ModelsStoreEntry{}}
}

func (s *InMemoryModelsStore) Read(providerID string, ctx context.Context) (*ModelsStoreEntry, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[providerID]
	if !ok {
		return nil, nil
	}
	// structuredClone equivalent.
	enc, err := MarshalJSON(entry)
	if err != nil {
		return nil, err
	}
	var out ModelsStoreEntry
	if err := jsonUnmarshalStrict(enc, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *InMemoryModelsStore) Write(providerID string, entry *ModelsStoreEntry, ctx context.Context) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	enc, err := MarshalJSON(entry)
	if err != nil {
		return err
	}
	var cloned ModelsStoreEntry
	if err := jsonUnmarshalStrict(enc, &cloned); err != nil {
		return err
	}
	s.entries[providerID] = &cloned
	return nil
}

func (s *InMemoryModelsStore) Delete(providerID string, ctx context.Context) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, providerID)
	return nil
}
