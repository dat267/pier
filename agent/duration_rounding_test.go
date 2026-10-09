package agent

import (
	"testing"
	"time"
)

// Upstream agent-loop.ts rounds performance.now() deltas with Math.round.
func TestDurationMillisecondsRoundsToNearestMillisecond(t *testing.T) {
	for _, test := range []struct {
		elapsed time.Duration
		want    int64
	}{
		{elapsed: 1499 * time.Microsecond, want: 1},
		{elapsed: 1500 * time.Microsecond, want: 2},
		{elapsed: 2499 * time.Microsecond, want: 2},
		{elapsed: 2500 * time.Microsecond, want: 3},
	} {
		if got := roundDurationMilliseconds(test.elapsed); got != test.want {
			t.Errorf("roundDurationMilliseconds(%s) = %d, want %d", test.elapsed, got, test.want)
		}
	}
}
