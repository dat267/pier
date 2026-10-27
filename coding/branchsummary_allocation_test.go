package coding

import (
	"encoding/json"
	"fmt"
	"runtime"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.4.0 (4a080252), core/compaction/branch-summarization.ts
// prepareBranchEntries admits newest first but returns chronological messages.
// Compare allocation growth to catch Go prepends copying the growing slice.
// Decoder and race-instrumentation costs can differ, so an absolute per-message
// ceiling tests the environment rather than the collection's scaling (CR-075).
func TestBranchPreparationAllocationsStayLinear(t *testing.T) {
	const smallCount, largeCount = 512, 2048
	entries := make([]SessionEntry, largeCount)
	for i := range entries {
		entries[i] = SessionEntry{Type: "message", Message: json.RawMessage(fmt.Sprintf(`{"role":"user","content":"message-%d","timestamp":1}`, i))}
	}
	// Keep every message: the test measures allocation growth, not truncation.
	const tokenBudget = 1_000_000
	PrepareBranchEntries(entries[:1], tokenBudget)
	measure := func(input []SessionEntry) uint64 {
		t.Helper()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		preparation := PrepareBranchEntries(input, tokenBudget)
		runtime.ReadMemStats(&after)
		if len(preparation.Messages) != len(input) {
			t.Fatalf("retained %d messages, want %d", len(preparation.Messages), len(input))
		}
		for i, message := range preparation.Messages {
			user, ok := message.(*ai.UserMessage)
			if !ok || user.Content.Text != fmt.Sprintf("message-%d", i) {
				t.Fatalf("message %d out of chronological order: %#v", i, message)
			}
		}
		return after.TotalAlloc - before.TotalAlloc
	}
	small := measure(entries[:smallCount])
	large := measure(entries)
	// Four times the input may use up to six times the allocations, leaving
	// headroom for decoder/runtime variance. The former quadratic prepends
	// grow much faster at these sizes, even with linear decoding overhead.
	if small == 0 || large > small*6 {
		t.Fatalf("allocation growth: %d messages = %d bytes, %d messages = %d bytes; want at most 6x growth for 4x input", smallCount, small, largeCount, large)
	}
	t.Logf("allocation growth: %d messages = %d bytes, %d messages = %d bytes", smallCount, small, largeCount, large)
}
