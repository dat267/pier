package coding

import "testing"

// pi v1.5.2 (b4afdddf) agent-session.ts getNextCycleIndex, used by both the
// scoped and the available model cycles. A current model outside the list
// (index -1) cycles to the first entry forward and the last backward, so a
// one-entry list still selects its only model; a one-entry list with the model in
// it has nowhere to go.
func TestNextCycleIndex(t *testing.T) {
	cases := []struct {
		name       string
		current    int
		length     int
		direction  string
		want       int
		wantSwitch bool
	}{
		{name: "empty", current: -1, length: 0, direction: "forward"},
		{name: "absent-forward", current: -1, length: 2, direction: "forward", want: 0, wantSwitch: true},
		{name: "absent-backward", current: -1, length: 2, direction: "backward", want: 1, wantSwitch: true},
		{name: "absent-single-forward", current: -1, length: 1, direction: "forward", want: 0, wantSwitch: true},
		{name: "absent-single-backward", current: -1, length: 1, direction: "backward", want: 0, wantSwitch: true},
		{name: "present-single", current: 0, length: 1, direction: "forward"},
		{name: "present-forward", current: 0, length: 2, direction: "forward", want: 1, wantSwitch: true},
		{name: "present-wrap", current: 1, length: 2, direction: "forward", want: 0, wantSwitch: true},
		{name: "present-backward-wrap", current: 0, length: 3, direction: "backward", want: 2, wantSwitch: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			index, ok := nextCycleIndex(tc.current, tc.length, tc.direction)
			if ok != tc.wantSwitch || (ok && index != tc.want) {
				t.Fatalf("nextCycleIndex(%d, %d, %q) = %d, %v; want %d, %v",
					tc.current, tc.length, tc.direction, index, ok, tc.want, tc.wantSwitch)
			}
		})
	}
}
