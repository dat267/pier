package mcp

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dat267/pier/mcp/protocol"
)

// pi v1.5.2 (b4afdddf), transports/lines.ts readStdout: messages are assembled
// from stdout chunks that may split anywhere, including inside a rune, a CRLF
// termination is accepted, and blank lines are skipped.
func TestStdioTransportFramesSplitAndTerminatedLines(t *testing.T) {
	newTransport := func() (*StdioTransport, *[]Message) {
		transport := NewStdioTransport(StdioTransportOptions{})
		var messages []Message
		transport.OnMessage(func(message Message) { messages = append(messages, message) })
		transport.OnError(func(err error) { t.Errorf("unexpected error: %v", err) })
		return transport, &messages
	}

	t.Run("message split across reads", func(t *testing.T) {
		transport, messages := newTransport()
		full := []byte("{\"jsonrpc\":\"2.0\",\"method\":\"m\",\"params\":{\"text\":\"héllo\"}}\n")
		for index := range full {
			transport.handleStdout(full[index : index+1])
		}
		if len(*messages) != 1 {
			t.Fatalf("messages = %d, want the frame split at every byte", len(*messages))
		}
	})

	t.Run("utf8 sequence split across reads", func(t *testing.T) {
		transport, messages := newTransport()
		full := []byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"m\",\"params\":{\"text\":\"日本\"}}\n")
		// Split inside the first multi-byte rune so a per-chunk decode would
		// replace half of it.
		cut := bytes.IndexByte(full, 0xe6) + 1
		transport.handleStdout(full[:cut])
		transport.handleStdout(full[cut:])
		if len(*messages) != 1 {
			t.Fatalf("messages = %d, want 1", len(*messages))
		}
		request, ok := (*messages)[0].(*protocol.JsonRpcRequest)
		if !ok || !strings.Contains(string(request.Params), "日本") {
			t.Fatalf("message = %#v, want the decoded rune", (*messages)[0])
		}
	})

	t.Run("carriage return and blank lines", func(t *testing.T) {
		transport, messages := newTransport()
		transport.handleStdout([]byte("{\"jsonrpc\":\"2.0\",\"method\":\"ping\"}\r\n\n   \n"))
		if len(*messages) != 1 {
			t.Fatalf("messages = %d, want the CRLF frame only", len(*messages))
		}
	})

	t.Run("large message in many chunks", func(t *testing.T) {
		transport, messages := newTransport()
		payload := strings.Repeat("a", 1<<20)
		full := []byte("{\"jsonrpc\":\"2.0\",\"method\":\"m\",\"params\":{\"text\":\"" + payload + "\"}}\n")
		for start := 0; start < len(full); start += 64 * 1024 {
			end := start + 64*1024
			if end > len(full) {
				end = len(full)
			}
			transport.handleStdout(full[start:end])
		}
		if len(*messages) != 1 {
			t.Fatalf("messages = %d, want one 1 MiB frame", len(*messages))
		}
	})
}
