package coding

import (
	"bytes"
	ctxpkg "context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	if entry, err := store.Read("openai", ctx); err != nil || entry != nil {
		t.Fatalf("entry = %+v err = %v", entry, err)
	}
	if err := store.Write("openai", sampleEntry("gpt-5"), ctx); err != nil {
		t.Fatal(err)
	}
	entry, err := store.Read("openai", ctx)
	if err != nil || entry == nil || len(entry.Models) != 1 || entry.Models[0].ID != "gpt-5" {
		t.Fatalf("entry = %+v err = %v", entry, err)
	}
	if entry.Etag == nil || *entry.Etag != `"abc"` || entry.LastModified == nil || *entry.LastModified != 1700000000 {
		t.Fatalf("entry = %+v", entry)
	}

	// Reads clone, so mutating the returned entry cannot corrupt the store.
	entry.Models[0].ID = "mutated"
	deleteResult, _ := store.Read("openai", ctx)
	if deleteResult.Models[0].ID != "gpt-5" {
		t.Fatalf("stored entry was mutated: %+v", deleteResult)
	}

	// Writes clone too.
	original := sampleEntry("gpt-5-mini")
	if err := store.Write("openai-mini", original, ctx); err != nil {
		t.Fatal(err)
	}
	original.Models[0].ID = "mutated"
	stored, _ := store.Read("openai-mini", ctx)
	if stored.Models[0].ID != "gpt-5-mini" {
		t.Fatalf("stored entry = %+v", stored)
	}

	if err := store.Delete("openai", ctx); err != nil {
		t.Fatal(err)
	}
	if entry, _ := store.Read("openai", ctx); entry != nil {
		t.Fatalf("entry = %+v", entry)
	}

	// Cancellation fails fast.
	cancelled, cancel := ctxpkg.WithCancel(ctx)
	cancel()
	if _, err := store.Read("openai", cancelled); !errors.Is(err, ctxpkg.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if err := store.Write("openai", sampleEntry("x"), cancelled); !errors.Is(err, ctxpkg.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if err := store.Delete("openai", cancelled); !errors.Is(err, ctxpkg.Canceled) {
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
	if err := store.Write("deepseek", &ai.ModelsStoreEntry{LastModified: &zero}, ctxpkg.Background()); err != nil {
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
	if err := store.Write("openai", sampleEntry("gpt-5"), ctxpkg.Background()); err != nil {
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

	if entry, err := store.Read("openai", ctx); err != nil || entry != nil {
		t.Fatalf("entry = %+v err = %v", entry, err)
	}
	if err := store.Write("openai", sampleEntry("gpt-5"), ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Write("anthropic", sampleEntry("claude"), ctx); err != nil {
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
	entry, err := other.Read("openai", ctx)
	if err != nil || entry == nil || entry.Models[0].ID != "gpt-5" {
		t.Fatalf("entry = %+v err = %v", entry, err)
	}
	if entry.Etag == nil || *entry.Etag != `"abc"` {
		t.Fatalf("entry = %+v", entry)
	}

	// Deleting removes the provider from the file.
	if err := store.Delete("openai", ctx); err != nil {
		t.Fatal(err)
	}
	if entry, _ := store.Read("openai", ctx); entry != nil {
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
	if err := store.Write("openai", sampleEntry("gpt-5"), ctx); err != nil {
		t.Fatal(err)
	}

	// An externally replaced file is picked up via the revision.
	time.Sleep(5 * time.Millisecond)
	external := `{"external": {"models": [{"id": "ext", "name": "ext", "api": "openai-responses", "provider": "x", "baseUrl": "https://x", "reasoning": false, "input": ["text"], "cost": {"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0}, "contextWindow": 1000, "maxTokens": 100}]}}`
	if err := os.WriteFile(path, []byte(external), 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := store.Read("external", ctx)
	if err != nil || entry == nil || entry.Models[0].ID != "ext" {
		t.Fatalf("entry = %+v err = %v", entry, err)
	}
	if entry, _ := store.Read("openai", ctx); entry != nil {
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
			if err := store.Write(providerID, sampleEntry("model-"+providerID), ctx); err != nil {
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
		entry, err := store.Read(providerID, ctx)
		if err != nil || entry == nil || entry.Models[0].ID != "model-"+providerID {
			t.Fatalf("%s: entry = %+v err = %v", providerID, entry, err)
		}
	}

	// A missing store file reads as empty.
	empty := NewFileModelsStore(filepath.Join(dir, "missing.json"))
	if entry, err := empty.Read("openai", ctx); err != nil || entry != nil {
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
	if entry, err := store.Read("openai", ctx); err != nil || entry != nil {
		t.Fatalf("entry = %+v err = %v", entry, err)
	}
	// A BOM is stripped.
	if err := os.WriteFile(path, []byte("\uFEFF{\"openai\": {\"models\": []}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := store.Read("openai", ctx)
	if err != nil || entry == nil {
		t.Fatalf("entry = %+v err = %v", entry, err)
	}
}
