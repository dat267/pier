package coding

import (
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) packages/coding-agent/src/utils/ansi.ts
// `splitIncompleteAnsiSuffix`, used by core/bash-executor.ts to hold back a
// trailing escape sequence that a stdout chunk may split. Holding bytes back is
// only safe if the stream re-chunks the input without losing or duplicating a
// byte: the caller strips ANSI from the joined text later, so a dropped or
// repeated byte would corrupt command output depending on chunk boundaries.
var ansiSplitCorpus = []string{
	"plain text",
	"colored \x1b[31mred\x1b[0m text",
	"\x1b]0;title\x07after",
	"\x1b[<35;59;1Mmouse",
	"\x1b]0;" + strings.Repeat("a", 300) + "\x07 after",
	"\x1bPq#0;2;0;0;0\x1b\\dcs",
	"\u009b31mC1 red",
	"\x1b[38;2;1;2;3mtruecolor\x1b[0m",
	"tail incomplete \x1b[<35;59",
	"\x1b",
	"",
}

func TestSplitIncompleteAnsiSuffixPreservesBytes(t *testing.T) {
	for _, text := range ansiSplitCorpus {
		for split := 0; split <= len(text); split++ {
			complete, pending := splitIncompleteAnsiSuffix(text[:split])
			complete2, pending2 := splitIncompleteAnsiSuffix(pending + text[split:])
			if joined := complete + complete2 + pending2; joined != text {
				t.Fatalf("split %d of %q reconstructed %q", split, text, joined)
			}
			if !strings.HasPrefix(text, complete) || !strings.HasSuffix(text, pending2) {
				t.Fatalf("split %d of %q: complete %q is not a prefix or pending %q not a suffix", split, text, complete, pending2)
			}
		}
	}
}

// FuzzSplitIncompleteAnsiSuffixPreservesBytes drives the boundary at an
// arbitrary split point, which is exactly what a stdout chunk boundary does.
func FuzzSplitIncompleteAnsiSuffixPreservesBytes(f *testing.F) {
	for _, seed := range ansiSplitCorpus {
		f.Add(seed, 0)
		f.Add(seed, len(seed))
		for split := 0; split <= len(seed); split++ {
			f.Add(seed, split)
		}
	}
	f.Fuzz(func(t *testing.T, text string, split int) {
		split = max(split, 0)
		split = min(split, len(text))
		complete, pending := splitIncompleteAnsiSuffix(text[:split])
		complete2, pending2 := splitIncompleteAnsiSuffix(pending + text[split:])
		if joined := complete + complete2 + pending2; joined != text {
			t.Fatalf("split %d of %q reconstructed %q", split, text, joined)
		}
	})
}

// Reference: pi v1.5.2 utils/ansi.ts splitIncompleteAnsiSuffix slices the last
// MAX_PENDING_ANSI_LENGTH (256) UTF-16 code units, not bytes: an unfinished OSC
// whose non-ASCII payload is longer than 256 bytes but shorter than 256 units
// is still held back for the next chunk. A byte window emitted it instead, so a
// later chunk completes an OSC the terminal never saw start.
func TestSplitIncompleteAnsiSuffixWindowCountsCodeUnits(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		value string
	}{
		{"cjk 200", "\x1b]0;" + strings.Repeat("日", 200)},
		{"cjk 100", "\x1b]0;" + strings.Repeat("日", 100)},
		{"emoji title", "\x1b]0;title" + strings.Repeat("😀", 100)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			complete, pending := splitIncompleteAnsiSuffix(testCase.value)
			if complete != "" || pending != testCase.value {
				t.Fatalf("complete = %q, pending = %q; want the whole value pending", complete, pending)
			}
		})
	}
	// An ASCII OSC beyond the window is processed as-is, like the reference.
	ascii := "\x1b]0;" + strings.Repeat("a", 300)
	if complete, pending := splitIncompleteAnsiSuffix(ascii); complete != ascii || pending != "" {
		t.Fatalf("complete = %q, pending = %q; want no split", complete, pending)
	}
}
