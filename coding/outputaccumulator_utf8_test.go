package coding

import "testing"

// Reference: pi v1.4.0 (4a080252),
// packages/coding-agent/src/core/tools/output-accumulator.ts uses TextDecoder.
// Expected strings were checked with that pinned Node runtime.
func TestOutputAccumulatorZeroLimitsUseGoDefaults(t *testing.T) {
	// D310: unlike upstream options with explicit maxLines/maxBytes = 0, this
	// positional constructor treats zero as omitted and selects defaults.
	accumulator := NewOutputAccumulator(0, 0, "")
	if err := accumulator.Append([]byte("line\n")); err != nil {
		t.Fatal(err)
	}
	accumulator.Finish()
	defer accumulator.CloseTempFile()
	if got := accumulator.Snapshot(false).Truncation.Truncated; got {
		t.Fatal("zero limits should retain the Go constructor's default behavior")
	}
}

func TestOutputAccumulatorMalformedUTF8MatchesTextDecoder(t *testing.T) {
	for _, test := range []struct {
		name   string
		chunks [][]byte
		want   string
	}{
		{
			name:   "leading-continuations",
			chunks: [][]byte{{0x80, 0x81, 0x7f}},
			want:   "\uFFFD\uFFFD\x7f",
		},
		{
			name:   "reconsume-invalid-continuation",
			chunks: [][]byte{{0xe2, 0x28, 0xa1}},
			want:   "\uFFFD(\uFFFD",
		},
		{
			name:   "invalid-unicode-scalars",
			chunks: [][]byte{{0xe0, 0x80, 0x80, 0xed, 0xa0, 0x80, 0xf4, 0x90, 0x80, 0x80}},
			want:   "\uFFFD\uFFFD\uFFFD\uFFFD\uFFFD\uFFFD\uFFFD\uFFFD\uFFFD\uFFFD",
		},
		{
			name:   "incomplete-at-finish",
			chunks: [][]byte{{0xf0, 0x9f}},
			want:   "\uFFFD",
		},
		{
			name:   "split-invalid-continuation",
			chunks: [][]byte{{0xe2}, {0x28, 0xa1}},
			want:   "\uFFFD(\uFFFD",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			accumulator := NewOutputAccumulator(10, 64, "")
			for _, chunk := range test.chunks {
				if err := accumulator.Append(chunk); err != nil {
					t.Fatal(err)
				}
			}
			accumulator.Finish()
			defer accumulator.CloseTempFile()
			if got := accumulator.Snapshot(false).Content; got != test.want {
				t.Errorf("content = %q, want %q", got, test.want)
			}
		})
	}
}
