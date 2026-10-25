package mcp

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// pi v1.4.0 transports/in-memory.ts queues one microtask per send. The event
// loop delivers sequential sends in FIFO order, never concurrently.
func TestInMemoryTransportPreservesSendOrder(t *testing.T) {
	client, server := CreateInMemoryTransportPair()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	for _, transport := range []*InMemoryTransport{client, server} {
		if err := transport.Start(ctx); err != nil {
			t.Fatal(err)
		}
	}
	const count = 64
	received := make(chan string, count)
	server.OnMessage(func(message Message) { received <- message.(*protocol.JsonRpcNotification).Method })
	for index := range count {
		if err := client.Send(ctx, &protocol.JsonRpcNotification{JSONRPC: "2.0", Method: strconv.Itoa(index)}); err != nil {
			t.Fatal(err)
		}
	}
	for index := range count {
		select {
		case method := <-received:
			if want := strconv.Itoa(index); method != want {
				t.Fatalf("notification %d = %s, want %s (FIFO)", index, method, want)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
