package coding

import (
	"encoding/json"
	"fmt"
	"github.com/dat267/pier/ai"
	"runtime"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), core/compaction/branch-summarization.ts
// prepareBranchEntries admits newest first but returns chronological messages.
// The allocation counter catches Go prepends copying the whole growing slice;
// 512 tiny messages discriminate without benchmark loops or a timing gate.
func TestBranchPreparationAllocationsStayLinear(t *testing.T) {
	const count = 512
	entries := make([]SessionEntry, count)
	for i := range entries {
		entries[i] = SessionEntry{Type: "message", Message: json.RawMessage(fmt.Sprintf(`{"role":"user","content":"message-%d","timestamp":1}`, i))}
	}
	// Warm the message decoder's type metadata before counting allocations.
	PrepareBranchEntries(entries[:1], 10000)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	preparation := PrepareBranchEntries(entries, 10000)
	runtime.ReadMemStats(&after)
	bytes := after.TotalAlloc - before.TotalAlloc
	// More than 2 KiB per tiny message leaves ample decoder/GC overhead, but
	// excludes the former 2.3 MB of quadratic reference-slice copies.
	if bytes > count*2048 {
		t.Fatalf("preparing %d small messages allocated %d bytes, want at most %d", count, bytes, count*2048)
	}
	if len(preparation.Messages) != count {
		t.Fatalf("retained %d messages, want %d", len(preparation.Messages), count)
	}
	for i, message := range preparation.Messages {
		user, ok := message.(*ai.UserMessage)
		if !ok || user.Content.Text != fmt.Sprintf("message-%d", i) {
			t.Fatalf("message %d out of chronological order: %#v", i, message)
		}
	}
}
