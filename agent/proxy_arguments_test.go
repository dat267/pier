package agent

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// D353: the proxy previews tool arguments with the provider adapters' stream
// (upstream proxy.ts uses ToolCallArgumentsStream): a small call updates at
// every delta, and a large one ends with its exact value.
func TestProxyToolArgumentsPreviewIsThrottled(t *testing.T) {
	var small ai.ToolCallArgumentsStream
	small.Append(`{"command":"l`)
	if got := string(small.Append(`s"}`)); got != `{"command":"ls"}` {
		t.Fatalf("small preview = %s", got)
	}
	payload := `{"path":"a.txt","content":"` + strings.Repeat("x", 100*1024) + `"}`
	var args ai.ToolCallArgumentsStream
	var last []byte
	for i := 0; i < len(payload); i += 16 {
		last = args.Append(payload[i:min(i+16, len(payload))])
	}
	if string(last) != payload || args.Text() != payload {
		t.Fatalf("final preview is %d bytes, want the %d-byte text", len(last), len(payload))
	}
}
