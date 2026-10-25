package offloop

import "testing"

// Go-only D201 contract: unlike pi v1.4.0 interactive-mode.ts, Go queues must
// track overlapping workers explicitly. A completed key must become reusable,
// including the empty string, which GoCoalesced does not reserve or reject.
func TestCoalescedEmptyKeyBecomesReusable(t *testing.T) {
	q := New()
	t.Cleanup(q.Stop)
	var calls int
	q.GoCoalesced("", func() { calls++ })
	q.Flush()
	q.GoCoalesced("", func() { calls++ })
	q.Flush()
	if calls != 2 {
		t.Fatalf("completed empty-key task left key reserved: calls=%d, want 2", calls)
	}
}
