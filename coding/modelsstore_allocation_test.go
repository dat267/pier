package coding

import (
	"context"
	"runtime"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.4.0 (4a080252), packages/ai/src/models-store.ts read()
// uses structuredClone, not JSON stringify/parse. Count one warmed public Read
// to guard the cached-catalog startup path without a machine-speed threshold.
func TestModelsStoreReadCloneAllocationBudget(t *testing.T) {
	ctx := context.Background()
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
			if err := store.Write("test", entry, ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Read("test", ctx); err != nil {
				t.Fatal(err)
			}
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			cloned, err := store.Read("test", ctx)
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
