package mcp

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// Reference: pi v1.4.0 packages/mcp/src/transports/transport.ts keeps listeners
// in Sets. Registering another listener must not invalidate an earlier
// unsubscribe function when the Go listener slice grows.
func TestTransportUnsubscribeSurvivesListenerRegistration(t *testing.T) {
	transport, _ := CreateInMemoryTransportPair()
	first, second := 0, 0
	removeFirst := transport.OnError(func(error) { first++ })
	transport.OnError(func(error) { second++ })
	removeFirst()
	transport.EmitError(errors.New("probe"))
	if first != 0 || second != 1 {
		t.Fatalf("listener calls = (%d, %d), want (0, 1)", first, second)
	}
}

// pi v1.4.0 transports/transport.ts iterates a live Set, so removing a later
// listener during emit skips it. Go deliberately delivers its admitted snapshot.
func TestTransportMessageUnsubscribeDuringDispatchUsesSnapshot(t *testing.T) {
	client, server := CreateInMemoryTransportPair()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	defer client.Close(context.Background())
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var calls []string
	delivered := make(chan struct{}, 2)
	var removeSecond func()
	server.OnMessage(func(Message) {
		calls = append(calls, "first")
		removeSecond()
		delivered <- struct{}{}
	})
	removeSecond = server.OnMessage(func(Message) {
		calls = append(calls, "second")
		delivered <- struct{}{}
	})
	if err := client.Send(ctx, &protocol.JsonRpcNotification{JSONRPC: "2.0", Method: "probe"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-delivered:
		case <-ctx.Done():
			t.Fatalf("only delivered listeners %v", calls)
		}
	}
	if !reflect.DeepEqual(calls, []string{"first", "second"}) {
		t.Fatalf("callback snapshot = %v, want [first second]", calls)
	}
}

// Reference: pi v1.4.0 packages/mcp/src/client.ts onClose uses stable Set
// membership, just like TransportEvents.
func TestClientCloseUnsubscribeSurvivesListenerRegistration(t *testing.T) {
	client := NewClient(ClientOptions{})
	first, second := 0, 0
	removeFirst := client.OnClose(func() { first++ })
	client.OnClose(func() { second++ })
	removeFirst()
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if first != 0 || second != 1 {
		t.Fatalf("close listener calls = (%d, %d), want (0, 1)", first, second)
	}
}

// Reference: pi v1.4.0 packages/mcp/src/client.ts onError. Reproduce through a
// connected in-memory transport, not by invoking the client's private emitter.
func TestClientErrorUnsubscribeSurvivesListenerRegistration(t *testing.T) {
	client, server := connectTestClient(t)
	defer client.Close(context.Background())
	first, second := 0, 0
	removeFirst := client.OnError(func(error) { first++ })
	client.OnError(func(error) { second++ })
	removeFirst()
	server.transport.peer.EmitError(errors.New("probe"))
	if first != 0 || second != 1 {
		t.Fatalf("error listener calls = (%d, %d), want (0, 1)", first, second)
	}
}
