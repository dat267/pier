package coding

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// blockingCredentialStore holds every write until the test releases it, so a
// credential change can be in flight while the process settles.
type blockingCredentialStore struct {
	inner   ai.CredentialStore
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingCredentialStore() *blockingCredentialStore {
	return &blockingCredentialStore{
		inner: ai.NewInMemoryCredentialStore(), entered: make(chan struct{}), release: make(chan struct{}),
	}
}

func (s *blockingCredentialStore) block() {
	s.once.Do(func() { close(s.entered) })
	<-s.release
}

func (s *blockingCredentialStore) Read(providerID string, ctx context.Context) (*ai.Credential, error) {
	return s.inner.Read(providerID, ctx)
}

func (s *blockingCredentialStore) List(ctx context.Context) ([]ai.CredentialInfo, error) {
	return s.inner.List(ctx)
}

func (s *blockingCredentialStore) Modify(providerID string, fn func(*ai.Credential) (*ai.Credential, error), ctx context.Context) (*ai.Credential, error) {
	s.block()
	return s.inner.Modify(providerID, fn, ctx)
}

func (s *blockingCredentialStore) Delete(providerID string, ctx context.Context) error {
	s.block()
	return s.inner.Delete(providerID, ctx)
}

// pi v1.5.2 (b4afdddf) agent-session-runtime.ts dispose waits for credential
// writes in flight before the process exits (#11692): an OAuth refresh may
// already have spent the single-use refresh token, so exiting before the new one
// is saved loses the login.
func TestSettleCredentialWritesWaitsForPendingChanges(t *testing.T) {
	store := newBlockingCredentialStore()
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		Credentials: store, DisableModelsJSON: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		_ = runtime.Logout("anthropic", context.Background())
	}()
	<-store.entered

	settled := make(chan error, 1)
	go func() { settled <- runtime.SettleCredentialWrites(context.Background()) }()
	select {
	case err := <-settled:
		t.Fatalf("settle returned before the credential write finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(store.release)
	select {
	case err := <-settled:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("settle did not return after the credential write finished")
	}
	<-writeDone
}
