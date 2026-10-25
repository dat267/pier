package mcp

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

// pi v1.4.0 transports/streamable-http.ts consumeSseStream uses streaming
// TextDecoder: only the stream-leading BOM is stripped, and each malformed
// UTF-8 subsequence becomes a replacement character.
func TestConsumeSseStreamMatchesTextDecoder(t *testing.T) {
	for _, item := range []struct{ body, want string }{
		{"\ufeffdata: \ufeffhello\n\n", "\ufeffhello"},
		{"data: " + string([]byte{0xff, 0xff, 0xe2, 0x82}) + "\n\n", "\ufffd\ufffd\ufffd"},
	} {
		for _, reader := range []io.Reader{strings.NewReader(item.body), iotest.OneByteReader(strings.NewReader(item.body))} {
			var events []SseEvent
			err := ConsumeSseStream(reader, ConsumeSseOptions{OnEvent: func(event SseEvent) { events = append(events, event) }})
			if err != nil || len(events) != 1 || events[0].Data != item.want {
				t.Errorf("SSE bytes %x: events=%+v error=%v, want data=%q", []byte(item.body), events, err, item.want)
			}
		}
	}
}
