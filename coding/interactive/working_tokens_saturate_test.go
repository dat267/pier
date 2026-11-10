package interactive

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// The worked-for notice counts every prompt token a run's messages reported. A
// provider's usage fields are int64, and two messages can each report a value
// near the limit, so the run total wrapped negative and the notice rendered
// "↑-8446744073709551616" (D369/D402: the display sums fold through Number
// space and saturate at the representation bounds; 06be6972 fixed the same wrap
// in the session-info panel and compaction notices).
func TestWorkingTokenCountsSaturateOnOverflow(t *testing.T) {
	state := &InteractiveUIState{}
	state.StartWorkingClock(time.Now())
	huge := ai.Usage{Output: 10, CacheRead: 5_000_000_000_000_000_000}
	state.EndWorkingMessageUsage(huge)
	state.EndWorkingMessageUsage(huge)
	_, tokens, ok := state.StopWorkingClock(time.Now().Add(time.Second))
	if !ok {
		t.Fatal("the clock did not run")
	}
	rendered := tokens.String()
	if strings.Contains(rendered, "-") {
		t.Fatalf("the run's token total wrapped negative: %q", rendered)
	}
	if tokens.in != math.MaxInt64 || tokens.cacheRead != math.MaxInt64 {
		t.Fatalf("tokens = %+v, want both saturated at MaxInt64", tokens)
	}
	// The share still reads as a percentage of the counted input.
	if share := tokens.cacheShare(); !strings.HasSuffix(share, "%") {
		t.Fatalf("cacheShare = %q", share)
	}
}

// A single message whose own prompt fields already overflow must not wrap
// either, and ordinary counts are unchanged.
func TestWorkingTokenCountsKeepOrdinaryValues(t *testing.T) {
	counts := workingTokensOf(ai.Usage{Input: 100, CacheRead: 50, CacheWrite: 5, Output: 20})
	if counts.in != 155 || counts.out != 20 || counts.cacheRead != 50 {
		t.Fatalf("counts = %+v", counts)
	}
	if got := workingTokensOf(ai.Usage{Input: math.MaxInt64, CacheRead: math.MaxInt64}).in; got != math.MaxInt64 {
		t.Fatalf("single-message overflow = %d, want MaxInt64", got)
	}
}
