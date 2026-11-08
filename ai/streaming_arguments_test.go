package ai

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// D353: upstream v1.5.0 utils/streaming-json.ts parses each delta once and
// shares completed values between snapshots. A pier snapshot is contiguous
// JSON bytes, so each one copies the buffer: above 1 KiB the preview refreshes
// at most once a frame (16 ms), and always when the root object closes.
func TestStreamingArgumentsPreviewIsThrottled(t *testing.T) {
	payload := `{"path":"a.txt","content":"` + strings.Repeat("x", 100*1024) + `"}`
	// The clock advances 1 ms per reading: a large preview refreshes every
	// sixteenth delta.
	fakeStreamingJSONClock(t, int64(time.Millisecond))
	var args streamingArguments
	renders, maxLag := 0, 0
	var last []byte
	for i := 0; i < len(payload); i += 16 {
		end := min(i+16, len(payload))
		preview := args.Append(payload[i:end])
		if len(last) == 0 || &preview[0] != &last[0] {
			renders++
		}
		last = preview
		maxLag = max(maxLag, end-len(preview))
	}
	deltas := (len(payload) + 15) / 16
	if renders > 1024/16+deltas/16+2 {
		t.Fatalf("%d deltas rendered %d previews; want at most one per frame above 1 KiB", deltas, renders)
	}
	if maxLag > 16*16+8 {
		t.Fatalf("preview lagged %d bytes, more than a frame of deltas", maxLag)
	}
	if string(last) != payload {
		t.Fatalf("final preview differs from the complete text")
	}
	final, errText := args.Finish()
	if errText != "" || string(final) != payload {
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

// BenchmarkStreamingArgumentsLarge streams the D353 workloads in 16-byte
// deltas and finishes them: a 100 KiB write, a 200-edit call and a bash call.
func BenchmarkStreamingArgumentsLarge(b *testing.B) {
	var edits []string
	for i := range 200 {
		edits = append(edits, fmt.Sprintf(`{"oldText":"func f%d() {\n\treturn %d\n}","newText":"func f%d() int {\n\treturn %d * 2\n}"}`, i, i, i, i))
	}
	for _, workload := range []struct{ name, payload string }{
		{"write100k", `{"path":"a.txt","content":"` + strings.Repeat("x", 100*1024) + `"}`},
		{"edit200", `{"path":"src/main.go","edits":[` + strings.Join(edits, ",") + `]}`},
		{"bash", `{"command":"ls -la /tmp && echo \"done\"","timeout":30}`},
	} {
		b.Run(workload.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var args streamingArguments
				for i := 0; i < len(workload.payload); i += 16 {
					args.Append(workload.payload[i:min(i+16, len(workload.payload))])
				}
				_, _ = args.Finish()
			}
		})
	}
}
