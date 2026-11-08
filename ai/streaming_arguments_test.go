package ai

import (
	"strings"
	"testing"
)

// Upstream v1.5.0 streams every delta through an incremental parser. The port
// recomputes the preview only at 1 KiB, 2 KiB, ... so a large write's argument
// text stays linear instead of re-parsing and re-encoding the prefix per delta.
func TestStreamingArgumentsPreviewIsThrottled(t *testing.T) {
	payload := `{"path":"a.txt","content":"` + strings.Repeat("x", 100*1024) + `"}`
	var args streamingArguments
	last := ""
	changes := 0
	for i := 0; i < len(payload); i += 16 {
		end := min(i+16, len(payload))
		preview := args.Append(payload[i:end])
		if i >= streamingArgumentsPreviewBytes && string(preview) != last {
			changes++
		}
		last = string(preview)
	}
	if changes > 10 {
		t.Fatalf("100 KiB in 16-byte deltas recomputed the preview %d times above 4 KiB", changes)
	}
	final, errText := ParseToolCallArguments(args.String())
	if errText != "" || !strings.Contains(string(final), strings.Repeat("x", 100*1024)) {
		t.Fatalf("final arguments were not exact: err=%q len=%d", errText, len(final))
	}
}

// D353: below 1 KiB every delta still updates the preview, as the reference's
// incremental parser does, so a short streamed call (a bash command) shows
// each piece as it arrives.
func TestStreamingArgumentsSmallCallUpdatesEveryDelta(t *testing.T) {
	var args streamingArguments
	for _, step := range []struct{ delta, want string }{
		{`{"command":"l`, `{"command":"l"}`},
		{`s -la`, `{"command":"ls -la"}`},
		{`","timeout":5}`, `{"command":"ls -la","timeout":5}`},
	} {
		if got := string(args.Append(step.delta)); got != step.want {
			t.Fatalf("after %q preview = %s, want %s", step.delta, got, step.want)
		}
	}
}

func BenchmarkStreamingArgumentsLarge(b *testing.B) {
	payload := `{"path":"a.txt","content":"` + strings.Repeat("x", 100*1024) + `"}`
	b.ReportAllocs()
	for b.Loop() {
		var args streamingArguments
		for i := 0; i < len(payload); i += 16 {
			args.Append(payload[i:min(i+16, len(payload))])
		}
		_, _ = ParseToolCallArguments(args.String())
	}
}
