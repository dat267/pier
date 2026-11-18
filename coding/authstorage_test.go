package coding

import (
	"context"
	ctxpkg "context"
	"encoding/json"
	"errors"
	"github.com/dat267/pier/ai"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Round 96 tests: the auth.json credential storage (file/in-memory backends,
// read-only storage, reload coalescing) and getFileRevision.

func writeAuthFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAuthStorageFileBackendRoundTrip(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	storage := NewAuthStorage(authPath)
	ctx := ctxpkg.Background()

	// A missing file lists nothing.
	entries, err := storage.List(ctx)
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries = %+v err = %v", entries, err)
	}
	if _, err := os.Stat(authPath); err != nil {
		t.Fatalf("auth.json must be created: %v", err)
	}

	// Modify writes the credential.
	credential, err := storage.Modify("anthropic", func(current *ai.Credential) (*ai.Credential, error) {
		if current != nil {
			t.Fatalf("current = %+v", current)
		}
		return &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "sk-1"}}, nil
	}, ctx)
	if err != nil || credential.APIKey.Key != "sk-1" {
		t.Fatalf("credential = %+v err = %v", credential, err)
	}
	raw, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"anthropic\": {\n    \"type\": \"api_key\",\n    \"key\": \"sk-1\"\n  }\n}"
	if string(raw) != want {
		t.Fatalf("file = %q want %q", raw, want)
	}
	info, err := os.Stat(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}

	// Read resolves the stored value; a second store sees it (shared snapshot).
	read, err := storage.Read("anthropic", ctx)
	if err != nil || read.APIKey.Key != "sk-1" {
		t.Fatalf("read = %+v err = %v", read, err)
	}
	other := NewAuthStorage(authPath)
	if read, err := other.Read("anthropic", ctx); err != nil || read.APIKey.Key != "sk-1" {
		t.Fatalf("read = %+v err = %v", read, err)
	}

	// A returned nil leaves the entry unchanged.
	if _, err := storage.Modify("anthropic", func(current *ai.Credential) (*ai.Credential, error) { return nil, nil }, ctx); err != nil {
		t.Fatal(err)
	}
	if read, _ := storage.Read("anthropic", ctx); read.APIKey.Key != "sk-1" {
		t.Fatalf("read = %+v", read)
	}

	// An error from the callback propagates and writes nothing.
	boom := errors.New("no write")
	if _, err := storage.Modify("anthropic", func(current *ai.Credential) (*ai.Credential, error) { return nil, boom }, ctx); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if read, _ := storage.Read("anthropic", ctx); read.APIKey.Key != "sk-1" {
		t.Fatalf("read = %+v", read)
	}

	// Delete removes the provider.
	if err := storage.Delete("anthropic", ctx); err != nil {
		t.Fatal(err)
	}
	if read, _ := storage.Read("anthropic", ctx); read != nil {
		t.Fatalf("read = %+v", read)
	}
	entries, err = storage.List(ctx)
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries = %+v err = %v", entries, err)
	}
}

func TestAuthStorageConfiguredKeyResolution(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	writeAuthFile(t, authPath, `{"anthropic":{"type":"api_key","key":"$AUTH_TEST_KEY","env":{"AUTH_TEST_KEY":"from-env"}}}`)
	storage := NewAuthStorage(authPath)

	read, err := storage.Read("anthropic", ctxpkg.Background())
	if err != nil || read.APIKey.Key != "from-env" {
		t.Fatalf("read = %+v err = %v", read, err)
	}
	// List never resolves configured values.
	entries, err := storage.List(ctxpkg.Background())
	if err != nil || len(entries) != 1 || entries[0].ProviderID != "anthropic" || entries[0].Type != ai.CredentialAPIKey {
		t.Fatalf("entries = %+v err = %v", entries, err)
	}

	// The writable store resolves a command key by running it (the read-only
	// store is the one that returns it unresolved).
	writeAuthFile(t, authPath, `{"anthropic":{"type":"api_key","key":"!printf sk-cmd"}}`)
	command := NewAuthStorage(authPath)
	read, err = command.Read("anthropic", ctxpkg.Background())
	if err != nil || read.APIKey.Key != "sk-cmd" {
		t.Fatalf("read = %+v err = %v", read, err)
	}
	readOnly := NewReadOnlyAuthStorage(authPath)
	read, err = readOnly.Read("anthropic", ctxpkg.Background())
	if err != nil || read.APIKey.Key != "!printf sk-cmd" {
		t.Fatalf("read-only read = %+v err = %v", read, err)
	}
}

func TestAuthStorageExternalChange(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	writeAuthFile(t, authPath, `{"anthropic":{"type":"api_key","key":"sk-1"}}`)
	storage := NewAuthStorage(authPath)
	ctx := ctxpkg.Background()
	if read, _ := storage.Read("anthropic", ctx); read.APIKey.Key != "sk-1" {
		t.Fatalf("read = %+v", read)
	}

	// Change the file behind the store; the revision check picks it up.
	time.Sleep(5 * time.Millisecond)
	writeAuthFile(t, authPath, `{"anthropic":{"type":"api_key","key":"sk-2"},"openai":{"type":"api_key","key":"sk-3"}}`)
	if read, err := storage.Read("anthropic", ctx); err != nil || read.APIKey.Key != "sk-2" {
		t.Fatalf("read = %+v err = %v", read, err)
	}
	if read, err := storage.Read("openai", ctx); err != nil || read.APIKey.Key != "sk-3" {
		t.Fatalf("read = %+v err = %v", read, err)
	}
}

func TestAuthStorageInMemoryAndReadOnly(t *testing.T) {
	ctx := ctxpkg.Background()

	// The in-memory backend seeds data and refuses nothing.
	storage := AuthStorageInMemory(authStorageData{
		"openai": {Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "sk-in-memory"}},
	})
	if read, err := storage.Read("openai", ctx); err != nil || read.APIKey.Key != "sk-in-memory" {
		t.Fatalf("read = %+v err = %v", read, err)
	}
	if _, err := storage.Modify("openai", func(current *ai.Credential) (*ai.Credential, error) {
		return &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "sk-new"}}, nil
	}, ctx); err != nil {
		t.Fatal(err)
	}
	if read, _ := storage.Read("openai", ctx); read.APIKey.Key != "sk-new" {
		t.Fatalf("read = %+v", read)
	}

	// A backend without a file path reloads on every read.
	backend := NewInMemoryAuthStorageBackend()
	seeded := `{"openai":{"type":"api_key","key":"sk-1"}}`
	if _, err := backend.WithLock(func(current *string) (LockResult, error) {
		return LockResult{Next: &seeded}, nil
	}); err != nil {
		t.Fatal(err)
	}
	pathless := AuthStorageFromBackend(backend)
	if read, err := pathless.Read("openai", ctx); err != nil || read.APIKey.Key != "sk-1" {
		t.Fatalf("read = %+v err = %v", read, err)
	}
	updated := `{"openai":{"type":"api_key","key":"sk-2"}}`
	if _, err := backend.WithLock(func(current *string) (LockResult, error) {
		return LockResult{Next: &updated}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if read, err := pathless.Read("openai", ctx); err != nil || read.APIKey.Key != "sk-2" {
		t.Fatalf("read = %+v err = %v", read, err)
	}

	// The read-only storage parses and validates but never writes.
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	writeAuthFile(t, authPath, `{"anthropic":{"type":"api_key","key":"sk-readonly"},"openai":{"type":"oauth","access":"a","refresh":"r","expires":123}}`)
	readOnly := NewReadOnlyAuthStorage(authPath)
	read, err := readOnly.Read("anthropic", ctx)
	if err != nil || read.APIKey.Key != "sk-readonly" {
		t.Fatalf("read = %+v err = %v", read, err)
	}
	oauth, err := readOnly.Read("openai", ctx)
	if err != nil || oauth.Type != ai.CredentialOAuth || oauth.OAuth.Access != "a" {
		t.Fatalf("oauth = %+v err = %v", oauth, err)
	}
	entries, err := readOnly.List(ctx)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries = %+v err = %v", entries, err)
	}
	if _, err := readOnly.Modify("anthropic", func(current *ai.Credential) (*ai.Credential, error) { return nil, nil }, ctx); err == nil ||
		err.Error() != "Read-only credential storage cannot modify auth.json" {
		t.Fatalf("err = %v", err)
	}
	if err := readOnly.Delete("anthropic", ctx); err == nil ||
		err.Error() != "Read-only credential storage cannot modify auth.json" {
		t.Fatalf("err = %v", err)
	}
}

func TestReadOnlyAuthStorageValidation(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	ctx := ctxpkg.Background()

	// Invalid JSON.
	writeAuthFile(t, authPath, "not json")
	if _, err := NewReadOnlyAuthStorage(authPath).List(ctx); err == nil ||
		!strings.HasPrefix(err.Error(), "Failed to read auth.json:") {
		t.Fatalf("err = %v", err)
	}
	// Arrays are not objects.
	writeAuthFile(t, authPath, "[]")
	if _, err := NewReadOnlyAuthStorage(authPath).List(ctx); err == nil ||
		err.Error() != "Invalid auth.json: expected an object" {
		t.Fatalf("err = %v", err)
	}
	// A non-object credential.
	writeAuthFile(t, authPath, `{"anthropic":"nope"}`)
	if _, err := NewReadOnlyAuthStorage(authPath).List(ctx); err == nil ||
		err.Error() != `Invalid auth.json credential for provider "anthropic"` {
		t.Fatalf("err = %v", err)
	}
	// An api_key with a non-string key.
	writeAuthFile(t, authPath, `{"anthropic":{"type":"api_key","key":5}}`)
	if _, err := NewReadOnlyAuthStorage(authPath).List(ctx); err == nil {
		t.Fatal("numeric key must fail")
	}
	// An api_key with a non-string env value.
	writeAuthFile(t, authPath, `{"anthropic":{"type":"api_key","key":"k","env":{"A":1}}}`)
	if _, err := NewReadOnlyAuthStorage(authPath).List(ctx); err == nil {
		t.Fatal("numeric env must fail")
	}
	// An oauth credential missing expires.
	writeAuthFile(t, authPath, `{"openai":{"type":"oauth","access":"a","refresh":"r"}}`)
	if _, err := NewReadOnlyAuthStorage(authPath).List(ctx); err == nil {
		t.Fatal("missing expires must fail")
	}
	// An unknown type.
	writeAuthFile(t, authPath, `{"x":{"type":"other"}}`)
	if _, err := NewReadOnlyAuthStorage(authPath).List(ctx); err == nil {
		t.Fatal("unknown type must fail")
	}
	// A missing file is an empty store.
	if entries, err := NewReadOnlyAuthStorage(filepath.Join(dir, "missing.json")).List(ctx); err != nil || len(entries) != 0 {
		t.Fatalf("entries = %+v err = %v", entries, err)
	}
}

func TestReadStoredCredential(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	writeAuthFile(t, authPath, `{"anthropic":{"type":"api_key","key":"sk-stored"}}`)
	credential := ReadStoredCredential("anthropic", authPath)
	if credential == nil || credential.APIKey.Key != "sk-stored" {
		t.Fatalf("credential = %+v", credential)
	}
	if credential := ReadStoredCredential("missing", authPath); credential != nil {
		t.Fatalf("credential = %+v", credential)
	}
	if credential := ReadStoredCredential("anthropic", filepath.Join(dir, "nope.json")); credential != nil {
		t.Fatalf("credential = %+v", credential)
	}
}

func TestAuthStorageLockingAndCancellation(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	writeAuthFile(t, authPath, `{"anthropic":{"type":"api_key","key":"sk-1"}}`)
	storage := NewAuthStorage(authPath)

	// Concurrent modifies serialize; every distinct provider survives.
	var waitGroup sync.WaitGroup
	for index := range 8 {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			providerID := "provider-" + string(rune('a'+index))
			if _, err := storage.Modify(providerID, func(current *ai.Credential) (*ai.Credential, error) {
				return &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "sk-" + providerID}}, nil
			}, ctxpkg.Background()); err != nil {
				t.Errorf("modify: %v", err)
			}
		}(index)
	}
	waitGroup.Wait()

	raw, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if len(onDisk) != 9 {
		t.Fatalf("on disk = %v", onDisk)
	}

	// A cancelled context fails fast.
	cancelled, cancel := ctxpkg.WithCancel(ctxpkg.Background())
	cancel()
	if _, err := storage.Read("anthropic", cancelled); !errors.Is(err, ctxpkg.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if err := storage.Delete("anthropic", cancelled); !errors.Is(err, ctxpkg.Canceled) {
		t.Fatalf("err = %v", err)
	}

	// A held lock makes the sync path wait rather than corrupt the file.
	release, err := acquireAuthLockDir(authPath)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		// The sync path retries for ~200ms then succeeds once released.
		time.Sleep(50 * time.Millisecond)
		release.release()
	}()
	if _, err := storage.Modify("late", func(current *ai.Credential) (*ai.Credential, error) {
		return &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: "sk-late"}}, nil
	}, ctxpkg.Background()); err != nil {
		t.Fatalf("modify: %v", err)
	}
	<-done
	if read, err := storage.Read("late", ctxpkg.Background()); err != nil || read.APIKey.Key != "sk-late" {
		t.Fatalf("read = %+v err = %v", read, err)
	}
}

func TestAuthStorageRetriesTransientWindowsEPERMOnSyncLock(t *testing.T) {
	previousPlatform, previousMkdir := authLockPlatform, authLockMkdir
	t.Cleanup(func() { authLockPlatform, authLockMkdir = previousPlatform, previousMkdir })
	authLockPlatform = "windows"
	calls := 0
	authLockMkdir = func(path string, mode os.FileMode) error {
		calls++
		if calls == 1 {
			return &os.PathError{Op: "mkdir", Path: path, Err: syscall.EPERM}
		}
		return os.Mkdir(path, mode)
	}
	backend := NewFileAuthStorageBackend(filepath.Join(t.TempDir(), "auth.json"))
	if _, err := backend.WithLock(func(*string) (LockResult, error) { return LockResult{}, nil }); err != nil {
		t.Fatalf("WithLock error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("lock attempts = %d, want retry after transient EPERM", calls)
	}
}

func TestAuthStorageRetriesTransientWindowsEPERMOnAsyncLock(t *testing.T) {
	previousPlatform, previousMkdir := authLockPlatform, authLockMkdir
	t.Cleanup(func() { authLockPlatform, authLockMkdir = previousPlatform, previousMkdir })
	authLockPlatform = "windows"
	calls := 0
	authLockMkdir = func(path string, mode os.FileMode) error {
		calls++
		if calls == 1 {
			return &os.PathError{Op: "mkdir", Path: path, Err: syscall.EPERM}
		}
		return os.Mkdir(path, mode)
	}
	backend := NewFileAuthStorageBackend(filepath.Join(t.TempDir(), "auth.json"))
	if _, err := backend.WithLockAsync(ctxpkg.Background(), func(*string) (LockResult, error) {
		return LockResult{}, nil
	}); err != nil {
		t.Fatalf("WithLockAsync error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("lock attempts = %d, want retry after transient EPERM", calls)
	}
}

func TestAuthStorageBoundsWindowsEPERMRetriesAndRejectsItElsewhere(t *testing.T) {
	previousPlatform, previousMkdir := authLockPlatform, authLockMkdir
	t.Cleanup(func() { authLockPlatform, authLockMkdir = previousPlatform, previousMkdir })
	path := filepath.Join(t.TempDir(), "auth.json")
	eperm := &os.PathError{Op: "mkdir", Path: path + ".lock", Err: syscall.EPERM}
	backend := NewFileAuthStorageBackend(path)

	authLockPlatform = "windows"
	calls := 0
	authLockMkdir = func(string, os.FileMode) error { calls++; return eperm }
	_, err := backend.WithLockAsync(ctxpkg.Background(), func(*string) (LockResult, error) { return LockResult{}, nil })
	if !errors.Is(err, syscall.EPERM) || calls != authLockEpermRetries+1 {
		t.Fatalf("persistent Windows EPERM err=%v calls=%d", err, calls)
	}

	authLockPlatform = "linux"
	calls = 0
	_, err = backend.WithLockAsync(ctxpkg.Background(), func(*string) (LockResult, error) { return LockResult{}, nil })
	if !errors.Is(err, syscall.EPERM) || calls != 1 {
		t.Fatalf("Linux EPERM err=%v calls=%d", err, calls)
	}
}

func TestAuthLockStaleness(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "auth.json")
	// A stale lock directory is reclaimed.
	lockPath := authLockDir(target)
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	release, err := acquireAuthLockDir(target)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	release.release()
	if PathExists(lockPath) {
		t.Fatal("lock must be released")
	}

	// A fresh lock is respected.
	release, err = acquireAuthLockDir(target)
	if err != nil {
		t.Fatal(err)
	}
	defer release.release()
	if _, err := acquireAuthLockDir(target); !errors.Is(err, errAuthLocked) {
		t.Fatalf("err = %v", err)
	}
}

func TestGetFileRevision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	writeAuthFile(t, path, "one")
	first, ok := GetFileRevision(path)
	if !ok || first == "" {
		t.Fatalf("revision = %q ok = %v", first, ok)
	}
	// Rewriting the same content can still change the revision (mtime/ctime).
	time.Sleep(5 * time.Millisecond)
	writeAuthFile(t, path, "two")
	second, ok := GetFileRevision(path)
	if !ok || second == first {
		t.Fatalf("revision = %q", second)
	}
	if _, ok := GetFileRevision(filepath.Join(dir, "missing")); ok {
		t.Fatal("missing file must have no revision")
	}
}

// pi v1.5.2 (b4afdddf) core/auth-storage.ts dropped `realpath: false` from the
// lockfile calls, so symlinked aliases of auth.json share one lock (#11655).
// Locking the alias path next to the symlink instead let two writers hold
// "the" lock at once.
func TestAuthLockFollowsSymlinkedAliases(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real", "auth.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	aliasDir := filepath.Join(dir, "alias")
	if err := os.MkdirAll(aliasDir, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(aliasDir, "auth.json")
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	release, err := acquireAuthLockDir(target)
	if err != nil {
		t.Fatalf("lock target: %v", err)
	}
	defer release.release()
	if _, err := acquireAuthLockDir(alias); !errors.Is(err, errAuthLocked) {
		t.Fatalf("alias lock err = %v, want errAuthLocked", err)
	}
}

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

// pi v1.4.0 (4a080252), auth-storage.ts InMemoryAuthStorageBackend.withLockAsync
// checks signal.throwIfAborted after the callback and before committing next.
func TestMemoryAuthBackendDoesNotCommitCancelledCallback(t *testing.T) {
	backend := NewInMemoryAuthStorageBackend()
	original := "old"
	if _, err := backend.WithLock(func(*string) (LockResult, error) {
		return LockResult{Next: &original}, nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := backend.WithLockAsync(ctx, func(*string) (LockResult, error) {
		cancel()
		next := "new"
		return LockResult{Next: &next}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("callback cancellation = %v, want context.Canceled", err)
	}
	// The synchronous read also waits until the worker releases its data lock,
	// so the check observes the settled operation even if cancellation won early.
	value, err := backend.WithLock(func(current *string) (LockResult, error) {
		if current == nil {
			return LockResult{}, nil
		}
		return LockResult{Result: *current}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if value != "old" {
		t.Fatalf("cancelled callback committed %v, want old state", value)
	}
}

// pi v1.4.0 (4a080252), core/auth-storage.ts withLockAsync checks
// onCompromised before committing; proper-lockfile owns its lock lease. A
// suspended process may resume after another process reclaimed its stale lock.
// The old operation must neither commit nor remove the successor's live lock.
func TestAuthStorageLostLeaseDoesNotCommitOrReleaseSuccessor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	const original = `{"original":{"type":"api_key","key":"synthetic-original"}}`
	writeAuthFile(t, path, original)
	backend := NewFileAuthStorageBackend(path)
	entered, finishFirst := make(chan struct{}), make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := backend.WithLockAsync(context.Background(), func(*string) (LockResult, error) {
			close(entered)
			<-finishFirst
			stale := `{"stale":{"type":"api_key","key":"synthetic-stale"}}`
			return LockResult{Next: &stale}, nil
		})
		firstDone <- err
	}()
	<-entered
	// Model stale recovery without waiting 30 seconds. Retain the old inode
	// so an immediately recreated directory cannot reuse its identity.
	lockPath := path + ".lock"
	if err := os.Rename(lockPath, lockPath+".expired"); err != nil {
		close(finishFirst)
		<-firstDone
		t.Fatal(err)
	}
	secondEntered, finishSecond := make(chan struct{}), make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		_, err := NewFileAuthStorageBackend(path).WithLockAsync(context.Background(), func(*string) (LockResult, error) {
			close(secondEntered)
			<-finishSecond
			current := `{"current":{"type":"api_key","key":"synthetic-current"}}`
			return LockResult{Next: &current}, nil
		})
		secondDone <- err
	}()
	<-secondEntered
	close(finishFirst)
	firstErr := <-firstDone
	raw, readErr := os.ReadFile(path)
	_, lockErr := os.Stat(lockPath)
	close(finishSecond)
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	if firstErr == nil {
		t.Error("operation committed after losing its lock lease")
	}
	if readErr != nil || string(raw) != original {
		t.Errorf("old holder changed auth.json while successor owned the lock: %q, %v", raw, readErr)
	}
	if lockErr != nil {
		t.Errorf("old holder removed successor's lock: %v", lockErr)
	}
}

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
			got := readFileBytes(t, path)
			if string(got) != tc.Expected {
				t.Fatalf("auth.json = %s; want %s", got, tc.Expected)
			}
		})
	}
}
