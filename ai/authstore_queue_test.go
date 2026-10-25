package ai

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// pi v1.4.0 (4a080252), auth/credential-store.ts enqueue serializes only
// writes for the same provider, and read/list never wait for user callbacks.
func TestInMemoryCredentialStoreOtherProviderProgresses(t *testing.T) {
	store := NewInMemoryCredentialStore()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	held := make(chan error, 1)
	go func() {
		_, err := store.Modify("held", func(*Credential) (*Credential, error) { close(entered); <-release; return nil, nil }, ctx)
		held <- err
	}()
	<-entered
	defer func() { close(release); <-held }()
	done := make(chan error, 1)
	go func() {
		if _, err := store.Read("held", ctx); err != nil {
			done <- err
			return
		}
		if _, err := store.List(ctx); err != nil {
			done <- err
			return
		}
		if _, err := store.Modify("other", func(*Credential) (*Credential, error) { return testAPIKeyCredential("other"), nil }, ctx); err != nil {
			done <- err
			return
		}
		done <- store.Delete("other", ctx)
	}()
	if err := awaitCredentialOperation(t, ctx, done); err != nil {
		t.Fatal(err)
	}
}

// pi v1.4.0 (4a080252), auth/credential-store.ts chains modify and delete
// together in admission order, even if a prior callback fails.
func TestInMemoryCredentialStoreWriteOrder(t *testing.T) {
	store := NewInMemoryCredentialStore()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	first := make(chan error, 1)
	go func() {
		_, err := store.Modify("p", func(*Credential) (*Credential, error) {
			close(entered)
			<-release
			return testAPIKeyCredential("first"), nil
		}, ctx)
		first <- err
	}()
	<-entered
	secondCtx := newCredentialWaitContext(ctx)
	second := make(chan error, 1)
	go func() {
		_, err := store.Modify("p", func(current *Credential) (*Credential, error) {
			if current == nil || current.APIKey.Key != "first" {
				return nil, errors.New("second write did not see first commit")
			}
			return testAPIKeyCredential("second"), nil
		}, secondCtx)
		second <- err
	}()
	awaitCredentialAdmission(t, ctx, secondCtx.waiting)
	deleteCtx := newCredentialWaitContext(ctx)
	deleted := make(chan error, 1)
	go func() { deleted <- store.Delete("p", deleteCtx) }()
	awaitCredentialAdmission(t, ctx, deleteCtx.waiting)
	releaseOnce.Do(func() { close(release) })
	for _, done := range []<-chan error{first, second, deleted} {
		if err := awaitCredentialOperation(t, ctx, done); err != nil {
			t.Fatal(err)
		}
	}
	if saved, err := store.Read("p", ctx); err != nil || saved != nil {
		t.Fatalf("post-delete credential = %+v, error = %v", saved, err)
	}
	sentinel := errors.New("callback failed")
	if _, err := store.Modify("p", func(*Credential) (*Credential, error) { return nil, sentinel }, ctx); !errors.Is(err, sentinel) {
		t.Fatalf("callback error = %v", err)
	}
	if _, err := store.Modify("p", func(*Credential) (*Credential, error) { return testAPIKeyCredential("after-error"), nil }, ctx); err != nil {
		t.Fatal(err)
	}
}

// pi v1.4.0 (4a080252), auth/credential-store.ts raceWithAbortSignal lets
// a queued caller leave, but its chain position cannot release active work.
func TestInMemoryCredentialStoreCancelledWaitKeepsWriteOrder(t *testing.T) {
	for _, deletion := range []bool{false, true} {
		name := "modify"
		if deletion {
			name = "delete"
		}
		t.Run(name, func(t *testing.T) {
			store := NewInMemoryCredentialStore()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			first := make(chan error, 1)
			go func() {
				_, err := store.Modify("p", func(*Credential) (*Credential, error) {
					close(entered)
					<-release
					return testAPIKeyCredential("first"), nil
				}, ctx)
				first <- err
			}()
			<-entered
			waitingParent, cancelWaiting := context.WithCancel(ctx)
			defer cancelWaiting()
			waitingCtx := newCredentialWaitContext(waitingParent)
			called := make(chan struct{}, 1)
			waiting := make(chan error, 1)
			go func() {
				if deletion {
					waiting <- store.Delete("p", waitingCtx)
					return
				}
				_, err := store.Modify("p", func(*Credential) (*Credential, error) {
					called <- struct{}{}
					return testAPIKeyCredential("cancelled"), nil
				}, waitingCtx)
				waiting <- err
			}()
			awaitCredentialAdmission(t, ctx, waitingCtx.waiting)
			cancelWaiting()
			if err := awaitCredentialOperation(t, ctx, waiting); !errors.Is(err, context.Canceled) {
				t.Fatalf("queued error = %v, want cancellation", err)
			}
			lastCtx := newCredentialWaitContext(ctx)
			last := make(chan error, 1)
			go func() {
				_, err := store.Modify("p", func(current *Credential) (*Credential, error) {
					if current == nil || current.APIKey.Key != "first" {
						return nil, errors.New("cancelled tail let the last write overtake the first")
					}
					return testAPIKeyCredential("last"), nil
				}, lastCtx)
				last <- err
			}()
			awaitCredentialAdmission(t, ctx, lastCtx.waiting)
			releaseOnce.Do(func() { close(release) })
			for _, done := range []<-chan error{first, last} {
				if err := awaitCredentialOperation(t, ctx, done); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-called:
				t.Fatal("cancelled callback ran")
			default:
			}
			saved, err := store.Read("p", ctx)
			if err != nil || saved == nil || saved.APIKey.Key != "last" {
				t.Fatalf("saved = %+v, error = %v", saved, err)
			}
		})
	}
}

func TestInMemoryCredentialStoreCancelledBeforeAdmission(t *testing.T) {
	store := NewInMemoryCredentialStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if _, err := store.Modify("p", func(*Credential) (*Credential, error) { called = true; return nil, nil }, ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("modify error = %v", err)
	}
	if err := store.Delete("p", ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("delete error = %v", err)
	}
	if called {
		t.Fatal("cancelled callback ran")
	}
}

func testAPIKeyCredential(key string) *Credential {
	return &Credential{Type: CredentialAPIKey, APIKey: &ApiKeyCredential{Key: key}}
}

// Done is observed only once a previous write exists and cancellation-aware
// admission starts waiting. This coordinates tests without polling or sleeps.
type credentialWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func newCredentialWaitContext(ctx context.Context) *credentialWaitContext {
	return &credentialWaitContext{Context: ctx, waiting: make(chan struct{})}
}
func (c *credentialWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}
func awaitCredentialAdmission(t *testing.T, ctx context.Context, waiting <-chan struct{}) {
	t.Helper()
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatal("credential write did not reach cancellable admission")
	}
}
func awaitCredentialOperation(t *testing.T, ctx context.Context, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		t.Fatal("credential operation stalled")
		return ctx.Err()
	}
}
