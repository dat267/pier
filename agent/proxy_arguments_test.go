package agent

import (
	"strings"
	"testing"
)

// D353: the proxy preview schedule matches the provider adapters; the final
// tool call still comes from the server event.
func TestProxyToolArgumentsPreviewIsThrottled(t *testing.T) {
	payload := `{"path":"a.txt","content":"` + strings.Repeat("x", 100*1024) + `"}`
	var args proxyToolArguments
	last := ""
	changes := 0
	for i := 0; i < len(payload); i += 16 {
		preview := string(args.Append(payload[i:min(i+16, len(payload))]))
		if preview != last {
			changes++
			last = preview
		}
	}
	if changes > 10 {
		t.Fatalf("100 KiB in 16-byte deltas recomputed the preview %d times", changes)
	}
}
