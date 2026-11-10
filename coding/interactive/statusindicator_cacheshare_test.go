package interactive

import "testing"

// cacheShare has no upstream counterpart (D416), but every other one-decimal
// display in this module formats through ai.JSNumberToFixed, matching JS
// toFixed, which rounds halfway cases away from zero (pi v1.5.0 f9a26365
// modes/interactive/components/footer.ts:27-29, :162). Go's strconv.FormatFloat
// rounds them to even, so a ratio landing exactly on a half printed one tenth
// low (D435).
func TestCacheShareRoundsHalvesLikeToFixed(t *testing.T) {
	for _, tc := range []struct {
		in        int64
		cacheRead int64
		want      string
	}{
		// Exact halves: 1/16 is 6.25%, which toFixed(1) prints as 6.3.
		{16, 1, "CH6.3%"},
		{32, 2, "CH6.3%"},
		{80, 1, "CH1.3%"},
		{400, 1, "CH0.3%"},
		{1600, 100, "CH6.3%"},
		// Non-halfway cases keep their value.
		{1000, 927, "CH92.7%"},
		{3, 1, "CH33.3%"},
		{3, 2, "CH66.7%"},
		{8, 5, "CH62.5%"},
		// A provider reporting more cached than counted input clamps at 100.
		{1000, 1500, "CH100.0%"},
		{1, 2, "CH100.0%"},
		// No cache activity or no input reports nothing.
		{1000, 0, ""},
		{0, 0, ""},
	} {
		got := workingTokenCount{cacheRead: tc.cacheRead, in: tc.in}.cacheShare()
		if got != tc.want {
			t.Errorf("cacheShare(in=%d, cacheRead=%d) = %q, want %q", tc.in, tc.cacheRead, got, tc.want)
		}
	}
}
