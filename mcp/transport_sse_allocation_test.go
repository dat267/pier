package mcp

import (
	"runtime"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), packages/mcp/src/transports/
// streamable-http.ts consumeSseStream accepts long data lines within its cap.
// This measures allocation traffic, not retained heap or wall-clock latency.
func TestConsumeSseLongLineAllocationsStayLinear(t *testing.T) {
	const size = 256 * 1024
	payload := strings.Repeat("x", size)
	body := "data: " + payload + "\n\n"
	var events []SseEvent
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	err := ConsumeSseStream(strings.NewReader(body), ConsumeSseOptions{OnEvent: func(event SseEvent) { events = append(events, event) }})
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Data != payload {
		t.Fatalf("events = %d, want one exact %d-byte payload", len(events), size)
	}
	if bytes := after.TotalAlloc - before.TotalAlloc; bytes > 12*size {
		t.Fatalf("parsing %d-byte line allocated %d bytes, want at most %d", size, bytes, 12*size)
	}
}
