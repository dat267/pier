package ai

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), src/model-catalog.ts and
// src/providers/openai.models.ts import the generated JSON and flatten it
// without serializing every model again. Most generated descriptors have no
// sampling parameters. Count allocations, not elapsed time, to prevent repeated
// JSON passes from returning to the interactive startup path.
func TestCatalogDecodeAllocationBudget(t *testing.T) {
	const count = 64
	var input strings.Builder
	input.WriteString(`{"providers":{"test":{"openai-completions":{`)
	for i := 0; i < count; i++ {
		if i > 0 {
			input.WriteByte(',')
		}
		fmt.Fprintf(&input, `"model-%d":{"id":"model-%d","name":"Test model","provider":"test","baseUrl":"https://example.invalid/v1","reasoning":false,"input":["text","image"],"cost":{"input":1,"output":2,"cacheRead":0,"cacheWrite":0},"contextWindow":128000,"maxTokens":8192,"compat":{"supportsStore":false}}`, i, i)
	}
	input.WriteString(`}}}}`)
	data := []byte(input.String())
	// Warm JSON type metadata before measuring a single decode.
	if _, _, err := decodeCatalog(data); err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, models, err := decodeCatalog(data)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if len(models["test"]) != count {
		t.Fatalf("decoded %d models, want %d", len(models["test"]), count)
	}
	allocations := after.Mallocs - before.Mallocs
	// Leave headroom above direct decoding for runtime variance, but exclude
	// the former raw-map round trip and unconditional sampling-order scans.
	if allocations > count*40 {
		t.Fatalf("decoding %d models allocated %d objects, want at most %d", count, allocations, count*40)
	}
}
