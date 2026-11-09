package agent

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// A proxy server event names the content block it applies to. The reference
// stores it as a sparse JavaScript array entry, so an out-of-range index costs
// one slot; the port's grow() materializes every slot in between, so one event
// with a large index allocates that many interfaces before any validation runs
// (measured: a single text_start with index 50,000,000 allocated 4.9 GB). A
// proxy server is untrusted input: it can be a remote endpoint, and a
// compromised or buggy one must not be able to exhaust the agent's memory from
// one line of JSON.
func TestProxyRejectsAnOutOfRangeContentIndex(t *testing.T) {
	const answerBlocks = 10_000
	partial := &ai.AssistantMessage{}
	scratch := newProxyEventScratch()

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := processProxyEvent(&proxyWireEvent{Type: "text_start", ContentIndex: 50_000_000}, partial, scratch)
	runtime.ReadMemStats(&after)

	if err == nil {
		t.Fatalf("a content index past the cap was accepted (content blocks: %d)", len(partial.Content))
	}
	if !strings.Contains(err.Error(), "content index") {
		t.Fatalf("error %q does not name the content index", err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
		t.Fatalf("one event allocated %d bytes before the index was rejected", allocated)
	}
	if len(partial.Content) > answerBlocks {
		t.Fatalf("partial grew to %d content blocks", len(partial.Content))
	}
}

// An answer never carries more blocks than a model emits: text, thinking and
// tool calls are bounded by the message the server finishes with. The cap
// leaves room above any real answer while still bounding the allocation.
func TestProxyAcceptsARealisticBlockCount(t *testing.T) {
	partial := &ai.AssistantMessage{}
	scratch := newProxyEventScratch()
	start := time.Now()
	for index := range 1000 {
		if _, err := processProxyEvent(&proxyWireEvent{Type: "text_start", ContentIndex: index}, partial, scratch); err != nil {
			t.Fatalf("text_start %d rejected: %v", index, err)
		}
	}
	if got := len(partial.Content); got != 1000 {
		t.Fatalf("content blocks = %d, want 1000", got)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("1000 starts took %v", elapsed)
	}
}
