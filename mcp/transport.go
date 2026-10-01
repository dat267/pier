// Package mcp is a Go port of @earendil-works/pi-mcp (pi/packages/mcp): an
// MCP client with stdio, in-memory, and streamable-HTTP transports.
//
// Ground truth: pi/packages/mcp/src at the pinned upstream commit.
//
// Upstream's promise/callback API maps to context.Context plus per-request
// channels here; the message shapes and validation live in the protocol
// package and stay byte-identical on the wire.
package mcp

import (
	"context"
	"sync"
)

// DefaultMaxMessageBytes is the default per-message JSON cap (upstream
// DEFAULT_MAX_MESSAGE_BYTES).
const DefaultMaxMessageBytes = 16 * 1024 * 1024

// Message is one JSON-RPC message on a transport: *protocol.JsonRpcRequest,
// *protocol.JsonRpcNotification, or *protocol.JsonRpcResponse.
type Message any

// Transport moves JSON-RPC messages to and from an MCP server (upstream
// McpTransport).
type Transport interface {
	Start(ctx context.Context) error
	Send(ctx context.Context, message Message) error
	Close(ctx context.Context) error
	OnMessage(listener func(Message)) (remove func())
	OnError(listener func(error)) (remove func())
	OnClose(listener func()) (remove func())
	// SetProtocolVersion is called with the version negotiated at
	// initialize; transports without a use for it ignore it.
	SetProtocolVersion(version string)
}

// messageEntry is one registered listener; removal marks it and emission
// compacts.
type listenerEntry[T any] struct {
	fn      T
	removed bool
}

// transportEvents is the listener bookkeeping shared by transports (upstream
// TransportEvents). emitClose fires at most once per transport. Listeners
// are snapshotted under the lock and delivered outside it.
type transportEvents struct {
	mu               sync.Mutex
	messageListeners []listenerEntry[func(Message)]
	errorListeners   []listenerEntry[func(error)]
	closeListeners   []listenerEntry[func()]
	closeEmitted     bool
}

// OnMessage registers a message listener; the returned func removes it.
func (b *transportEvents) OnMessage(listener func(Message)) func() {
	return b.register(&b.messageListeners, listener)
}

// OnError registers an error listener; the returned func removes it.
func (b *transportEvents) OnError(listener func(error)) func() {
	return b.register(&b.errorListeners, listener)
}

// OnClose registers a close listener; the returned func removes it.
func (b *transportEvents) OnClose(listener func()) func() {
	return b.register(&b.closeListeners, listener)
}

func (b *transportEvents) register[T any](set *[]listenerEntry[T], fn T) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	*set = append(*set, listenerEntry[T]{fn: fn})
	token := &(*set)[len(*set)-1]
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		token.removed = true
	}
}

// snapshotLocked collects the live listeners and compacts; the caller holds
// b.mu.
func snapshotLocked[T any](set *[]listenerEntry[T]) []T {
	live := make([]T, 0, len(*set))
	for _, entry := range *set {
		if !entry.removed {
			live = append(live, entry.fn)
		}
	}
	kept := (*set)[:0]
	for _, entry := range *set {
		if !entry.removed {
			kept = append(kept, entry)
		}
	}
	*set = kept
	return live
}

func (b *transportEvents) emitMessage(message Message) {
	b.mu.Lock()
	listeners := snapshotLocked(&b.messageListeners)
	b.mu.Unlock()
	for _, listener := range listeners {
		listener(message)
	}
}

func (b *transportEvents) emitError(err error) {
	b.mu.Lock()
	listeners := snapshotLocked(&b.errorListeners)
	b.mu.Unlock()
	for _, listener := range listeners {
		listener(err)
	}
}

func (b *transportEvents) emitClose() {
	b.mu.Lock()
	if b.closeEmitted {
		b.mu.Unlock()
		return
	}
	b.closeEmitted = true
	listeners := snapshotLocked(&b.closeListeners)
	b.mu.Unlock()
	for _, listener := range listeners {
		listener()
	}
}
