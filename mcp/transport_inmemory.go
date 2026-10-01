package mcp

import (
	"context"
	"encoding/json"

	"github.com/dat267/pier/mcp/protocol"
)

// InMemoryTransport is an in-process transport paired with a peer (upstream
// InMemoryTransport). Messages are delivered on their own goroutine, like
// upstream's queueMicrotask, and deep-copied so a mutation on one side is
// not visible on the other.
type InMemoryTransport struct {
	transportEvents
	peer    *InMemoryTransport
	started bool
	closed  bool
}

// ConnectPeer wires this transport to its peer; each side of the pair calls
// it once.
func (t *InMemoryTransport) ConnectPeer(peer *InMemoryTransport) {
	if t.peer != nil {
		panic("In-memory MCP transport already has a peer")
	}
	t.peer = peer
}

// CreateInMemoryTransportPair returns a connected client/server pair
// (upstream createInMemoryTransportPair).
func CreateInMemoryTransportPair() (client, server *InMemoryTransport) {
	client = &InMemoryTransport{}
	server = &InMemoryTransport{}
	client.ConnectPeer(server)
	server.ConnectPeer(client)
	return client, server
}

// Start marks the transport open.
func (t *InMemoryTransport) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return &protocol.McpConnectionClosedError{}
	}
	t.started = true
	return nil
}

// Send delivers a deep copy of the message to the peer's listeners
// asynchronously.
func (t *InMemoryTransport) Send(ctx context.Context, message Message) error {
	t.mu.Lock()
	started, closed, peer := t.started, t.closed, t.peer
	t.mu.Unlock()
	if !started || closed {
		return &protocol.McpConnectionClosedError{}
	}
	if peer == nil {
		return &protocol.McpConnectionClosedError{Msg: "In-memory MCP peer is not connected"}
	}
	peer.mu.Lock()
	peerStarted, peerClosed := peer.started, peer.closed
	peer.mu.Unlock()
	if !peerStarted || peerClosed {
		return &protocol.McpConnectionClosedError{Msg: "In-memory MCP peer is not connected"}
	}
	copy, err := cloneMessage(message)
	if err != nil {
		return err
	}
	go peer.deliver(copy)
	return nil
}

// Close closes this transport and then the peer's.
func (t *InMemoryTransport) Close(ctx context.Context) error {
	t.mu.Lock()
	alreadyClosed := t.closed
	t.closed = true
	peer := t.peer
	t.mu.Unlock()
	if alreadyClosed {
		return nil
	}
	t.emitClose()
	if peer != nil {
		_ = peer.Close(ctx)
	}
	return nil
}

// EmitError exposes transport-level failures to tests (upstream's protected
// override).
func (t *InMemoryTransport) EmitError(err error) { t.emitError(err) }

func (t *InMemoryTransport) deliver(message Message) {
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return
	}
	t.emitMessage(message)
}

// cloneMessage round-trips a message through JSON: every MCP message is a
// JSON value, and this reproduces structuredClone's isolation.
func cloneMessage(message Message) (Message, error) {
	data, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	return protocol.DecodeMessage(data)
}

// SetProtocolVersion is unused on in-memory transports.
func (t *InMemoryTransport) SetProtocolVersion(string) {}
