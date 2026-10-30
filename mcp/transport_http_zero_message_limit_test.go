package mcp

import (
	"io"
	"strings"
	"testing"
)

// pi v1.4.0 streamable-http.ts uses ?? for both maxEventBytes and
// maxMessageBytes, so explicit zero rejects any nonempty SSE data.
func TestConsumeSseStreamExplicitZeroEventLimit(t *testing.T) {
	err := ConsumeSseStream(strings.NewReader("data: x\n\n"), ConsumeSseOptions{
		MaxEventBytes:    0,
		MaxEventBytesSet: true,
		OnEvent:          func(SseEvent) {},
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds 0 bytes") {
		t.Fatalf("SSE parse error = %v, want zero-byte limit", err)
	}
}

func TestHttpTransportExplicitZeroMessageLimit(t *testing.T) {
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: "http://example.test/mcp", MaxMessageBytes: 0, MaxMessageBytesSet: true})
	err := transport.consumeSse(io.NopCloser(strings.NewReader("data: x\n\n")), &streamCursor{}, func(Message) {})
	if err == nil || !strings.Contains(err.Error(), "exceeds 0 bytes") {
		t.Fatalf("SSE transport error = %v, want zero-byte limit", err)
	}
}
