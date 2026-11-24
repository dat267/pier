package coding

import (
	"bytes"
	ctxpkg "context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// Round 97 tests: the file-backed and in-memory model catalog stores.

func sampleEntry(modelID string) *ai.ModelsStoreEntry {
	lastModified := int64(1700000000)
	etag := `"abc"`
	return &ai.ModelsStoreEntry{
		Models: []*ai.Model{{
			ID: modelID, Name: modelID, API: ai.APIOpenAIResponses, Provider: "openai",
			BaseURL: "https://api.openai.com/v1", Input: []string{"text"},
			ContextWindow: 128000, MaxTokens: 4096,
		}},
		LastModified: &lastModified,
		Etag:         &etag,
	}
}

func TestInMemoryModelsStore(t *testing.T) {
	ctx := ctxpkg.Background()
	store := NewInMemoryCodingAgentModelsStore()
	if entry, err := store.Read(ctx, "openai"); err != nil || entry != nil {
		t.Fatalf("entry = %+v err = %v", entry, err)
	}
	if err := store.Write(ctx, "openai", sampleEntry("gpt-5")); err != nil {
		t.Fatal(err)
	}
	entry, err := store.Read(ctx, "openai")
	if err != nil || entry == nil || len(entry.Models) != 1 || entry.Models[0].ID != "gpt-5" {
		t.Fatalf("entry = %+v err = %v", entry, err)
	}
	if entry.Etag == nil || *entry.Etag != `"abc"` || entry.LastModified == nil || *entry.LastModified != 1700000000 {
		t.Fatalf("entry = %+v", entry)
	}

	// Reads clone, so mutating the returned entry cannot corrupt the store.
	entry.Models[0].ID = "mutated"
	deleteResult, _ := store.Read(ctx, "openai")
	if deleteResult.Models[0].ID != "gpt-5" {
		t.Fatalf("stored entry was mutated: %+v", deleteResult)
	}

	// Writes clone too.
	original := sampleEntry("gpt-5-mini")
	if err := store.Write(ctx, "openai-mini", original); err != nil {
		t.Fatal(err)
	}
	original.Models[0].ID = "mutated"
	stored, _ := store.Read(ctx, "openai-mini")
	if stored.Models[0].ID != "gpt-5-mini" {
		t.Fatalf("stored entry = %+v", stored)
	}

	if err := store.Delete(ctx, "openai"); err != nil {
		t.Fatal(err)
	}
	if entry, _ := store.Read(ctx, "openai"); entry != nil {
		t.Fatalf("entry = %+v", entry)
	}

	// Cancellation fails fast.
	cancelled, cancel := ctxpkg.WithCancel(ctx)
	cancel()
	if _, err := store.Read(cancelled, "openai"); !errors.Is(err, ctxpkg.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if err := store.Write(cancelled, "openai", sampleEntry("x")); !errors.Is(err, ctxpkg.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if err := store.Delete(cancelled, "openai"); !errors.Is(err, ctxpkg.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

// pi v1.4.0 packages/ai/src/models-store.ts requires models to be an array.
// remote-catalog-provider.ts restores it with .filter(), so null breaks Pi when
// Pier shares ~/.pi/agent/models-store.json after an empty catalog refresh.
func TestFileModelsStoreEmptyCatalogIsArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models-store.json")
	store := NewFileModelsStore(path)
	zero := int64(0)
	if err := store.Write(ctxpkg.Background(), "deepseek", &ai.ModelsStoreEntry{LastModified: &zero}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if got := string(data["deepseek"]["models"]); got != "[]" {
		t.Fatalf("empty catalog must be [] for Pi compatibility, got %s", got)
	}
}

func TestFileModelsStoreWriteRepairsLegacyNullCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models-store.json")
	// Older Pier builds wrote this shape; any subsequent write must not carry
	// it back into the shared cache. Keep the old entry's metadata unchanged.
	if err := os.WriteFile(path, []byte(`{"deepseek":{"models":null,"lastModified":0,"checkedAt":123}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewFileModelsStore(path)
	if err := store.Write(ctxpkg.Background(), "openai", sampleEntry("gpt-5")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data["deepseek"]); err != nil {
		t.Fatal(err)
	}
	if got := compact.String(); got != `{"models":[],"lastModified":0,"checkedAt":123}` {
		t.Fatalf("legacy catalog was not repaired without losing metadata: %s", got)
	}
}

func TestFileModelsStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models-store.json")
	store := NewFileModelsStore(path)
	ctx := ctxpkg.Background()

	if entry, err := store.Read(ctx, "openai"); err != nil || entry != nil {
		t.Fatalf("entry = %+v err = %v", entry, err)
	}
	if err := store.Write(ctx, "openai", sampleEntry("gpt-5")); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(ctx, "anthropic", sampleEntry("claude")); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The file is JSON.stringify(data, null, 2) shaped and keeps write order.
	want := "{\n  \"openai\": {\n    \"models\": [\n      {\n        \"id\": \"gpt-5\","
	if !strings.HasPrefix(string(raw), want) {
		t.Fatalf("file = %q", raw)
	}
	if strings.Index(string(raw), `"openai": {`) > strings.Index(string(raw), `"anthropic": {`) {
		t.Fatalf("order = %q", raw)
	}

	// A second store on the same path sees the data (shared read state).
	other := NewFileModelsStore(path)
	entry, err := other.Read(ctx, "openai")
	if err != nil || entry == nil || entry.Models[0].ID != "gpt-5" {
		t.Fatalf("entry = %+v err = %v", entry, err)
	}
	if entry.Etag == nil || *entry.Etag != `"abc"` {
		t.Fatalf("entry = %+v", entry)
	}

	// Deleting removes the provider from the file.
	if err := store.Delete(ctx, "openai"); err != nil {
		t.Fatal(err)
	}
	if entry, _ := store.Read(ctx, "openai"); entry != nil {
		t.Fatalf("entry = %+v", entry)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"openai": {`) || !strings.Contains(string(raw), `"anthropic": {`) {
		t.Fatalf("file = %q", raw)
	}
}

func TestFileModelsStoreExternalChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models-store.json")
	store := NewFileModelsStore(path)
	ctx := ctxpkg.Background()
	if err := store.Write(ctx, "openai", sampleEntry("gpt-5")); err != nil {
		t.Fatal(err)
	}

	// An externally replaced file is picked up via the revision.
	time.Sleep(5 * time.Millisecond)
	external := `{"external": {"models": [{"id": "ext", "name": "ext", "api": "openai-responses", "provider": "x", "baseUrl": "https://x", "reasoning": false, "input": ["text"], "cost": {"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0}, "contextWindow": 1000, "maxTokens": 100}]}}`
	if err := os.WriteFile(path, []byte(external), 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := store.Read(ctx, "external")
	if err != nil || entry == nil || entry.Models[0].ID != "ext" {
		t.Fatalf("entry = %+v err = %v", entry, err)
	}
	if entry, _ := store.Read(ctx, "openai"); entry != nil {
		t.Fatalf("stale entry = %+v", entry)
	}
}

func TestFileModelsStoreConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models-store.json")
	store := NewFileModelsStore(path)
	ctx := ctxpkg.Background()

	var waitGroup sync.WaitGroup
	for index := range 8 {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			providerID := "provider-" + string(rune('a'+index))
			if err := store.Write(ctx, providerID, sampleEntry("model-"+providerID)); err != nil {
				t.Errorf("write: %v", err)
			}
		}(index)
	}
	waitGroup.Wait()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if len(onDisk) != 8 {
		t.Fatalf("on disk = %v", onDisk)
	}
	for index := range 8 {
		providerID := "provider-" + string(rune('a'+index))
		entry, err := store.Read(ctx, providerID)
		if err != nil || entry == nil || entry.Models[0].ID != "model-"+providerID {
			t.Fatalf("%s: entry = %+v err = %v", providerID, entry, err)
		}
	}

	// A missing store file reads as empty.
	empty := NewFileModelsStore(filepath.Join(dir, "missing.json"))
	if entry, err := empty.Read(ctx, "openai"); err != nil || entry != nil {
		t.Fatalf("entry = %+v err = %v", entry, err)
	}
}

func TestFileModelsStoreParseErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models-store.json")
	store := NewFileModelsStore(path)
	ctx := ctxpkg.Background()

	// Invalid JSON reads as empty rather than failing (upstream JSON.parse
	// would throw; the reload failure keeps the last valid snapshot).
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if entry, err := store.Read(ctx, "openai"); err != nil || entry != nil {
		t.Fatalf("entry = %+v err = %v", entry, err)
	}
	// A BOM is stripped.
	if err := os.WriteFile(path, []byte("\uFEFF{\"openai\": {\"models\": []}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := store.Read(ctx, "openai")
	if err != nil || entry == nil {
		t.Fatalf("entry = %+v err = %v", entry, err)
	}
}

// pi v1.4.0 core/models-store.ts parses the file with JSON.parse and does not
// catch, so a malformed store makes writes throw and the file is never
// rewritten. Pier's read path tolerates malformed content as empty (see
// TestFileModelsStoreParseErrors), but a write must not treat that tolerance
// as proof the other providers are gone: it rejects the store and leaves it
// untouched for the user to repair (D282).
func TestFileModelsStoreWriteRejectsMalformedAndNonObjectJSON(t *testing.T) {
	for _, corrupt := range []string{
		`{"anthropic":{"models":[]},"broken":`,
		`null`,
		`[]`,
		`{"anthropic":{"models":[]}}trailing`,
	} {
		path := filepath.Join(t.TempDir(), "models-store.json")
		if err := os.WriteFile(path, []byte(corrupt), 0o600); err != nil {
			t.Fatal(err)
		}
		store := NewFileModelsStore(path)
		if err := store.Write(ctxpkg.Background(), "openai", sampleEntry("gpt-5")); err == nil {
			t.Fatalf("%q: write accepted a corrupt store", corrupt)
		}
		if err := store.Delete(ctxpkg.Background(), "anthropic"); err == nil {
			t.Fatalf("%q: delete accepted a corrupt store", corrupt)
		}
		if after, err := os.ReadFile(path); err != nil || string(after) != corrupt {
			t.Fatalf("%q: file = %q, err = %v; want it untouched", corrupt, after, err)
		}
		// The tolerant read still reports no entry rather than failing startup.
		if entry, err := store.Read(ctxpkg.Background(), "anthropic"); err != nil || entry != nil {
			t.Fatalf("%q: read = %+v, %v; want the tolerant empty read", corrupt, entry, err)
		}
	}
}

// failingLockBackend fails every locked operation.
type failingLockBackend struct{ err error }

func (b failingLockBackend) WithLock(func(*string) (LockResult, error)) (any, error) {
	return nil, b.err
}

func (b failingLockBackend) WithLockAsync(ctxpkg.Context, func(*string) (LockResult, error)) (any, error) {
	return nil, b.err
}

// pi v1.4.0 core/models-store.ts readLatest awaits the shared reload promise,
// so a lock or read failure rejects the read; it does not fall back to the old
// snapshot.
func TestFileModelsStorePropagatesReloadFailure(t *testing.T) {
	store := NewFileModelsStore(filepath.Join(t.TempDir(), "models-store.json"))
	failure := errors.New("lock timeout")
	store.storage = failingLockBackend{err: failure}
	if entry, err := store.Read(ctxpkg.Background(), "openai"); !errors.Is(err, failure) || entry != nil {
		t.Fatalf("read = %+v, %v; want the reload failure", entry, err)
	}
}

// Reference: pi v1.4.0 (4a080252), packages/ai/src/models-store.ts read()
// uses structuredClone, not JSON stringify/parse. Count one warmed public Read
// to guard the cached-catalog startup path without a machine-speed threshold.
func TestModelsStoreReadCloneAllocationBudget(t *testing.T) {
	ctx := ctxpkg.Background()
	const count = 32
	entry := &ai.ModelsStoreEntry{Models: make([]*ai.Model, count)}
	for i := range entry.Models {
		entry.Models[i] = &ai.Model{
			ID: "model", Name: "Model", API: ai.APIOpenAICompletions, Provider: "test",
			Input: []string{"text", "image"}, Headers: map[string]string{"test": "value"},
			SamplingParams:      ai.SamplingParams{"z": []byte(`1`), "a": []byte(`2`)},
			SamplingParamsOrder: []string{"z", "a"},
		}
	}
	stores := map[string]ai.ModelsStore{
		"coding-memory": NewInMemoryCodingAgentModelsStore(),
		"ai-memory":     ai.NewInMemoryModelsStore(),
		"file":          NewFileModelsStore(t.TempDir() + "/models-store.json"),
	}
	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			if err := store.Write(ctx, "test", entry); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Read(ctx, "test"); err != nil {
				t.Fatal(err)
			}
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			cloned, err := store.Read(ctx, "test")
			runtime.ReadMemStats(&after)
			if err != nil {
				t.Fatal(err)
			}
			if cloned == nil || len(cloned.Models) != count {
				t.Fatalf("read did not return %d models", count)
			}
			if allocations := after.Mallocs - before.Mallocs; allocations > count*150 {
				t.Fatalf("reading %d cached models allocated %d objects, want at most %d", count, allocations, count*150)
			}
		})
	}
}
