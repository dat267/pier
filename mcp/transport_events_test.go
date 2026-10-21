package mcp

import (
	"context"
	"errors"
	"testing"
)

// Reference: pi v1.3.0 packages/mcp/src/transports/transport.ts keeps listeners
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

// Reference: pi v1.3.0 packages/mcp/src/client.ts onClose uses stable Set
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

// Reference: pi v1.3.0 packages/mcp/src/client.ts onError. Reproduce through a
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
