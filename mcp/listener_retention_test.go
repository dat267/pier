package mcp_test

import (
	"encoding/json"
	"runtime"
	"testing"
	"weak"

	"github.com/dat267/pier/mcp"
)

// pi v1.4.0 (4a080252), transports/transport.ts and client.ts unsubscribe
// with Set.delete. Removed listeners must release their captures while the
// transport/client itself is still alive, not just stop invoking the callback.
func TestUnsubscribedListenersReleaseCapturedState(t *testing.T) {
	transport, _ := mcp.CreateInMemoryTransportPair()
	client := mcp.NewClient(mcp.ClientOptions{})
	for _, test := range []struct {
		name     string
		register func(func()) func()
	}{
		{"transport message", func(fn func()) func() { return transport.OnMessage(func(mcp.Message) { fn() }) }},
		{"transport error", func(fn func()) func() { return transport.OnError(func(error) { fn() }) }},
		{"transport close", transport.OnClose},
		{"client notification", func(fn func()) func() { return client.OnNotification("probe", func(json.RawMessage) { fn() }) }},
		{"client error", func(fn func()) func() { return client.OnError(func(error) { fn() }) }},
		{"client close", client.OnClose},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := registerThenRemoveCapturedListener(test.register)
			runtime.GC()
			if payload.Value() != nil {
				t.Fatal("unsubscribed listener still retains its captured payload")
			}
			runtime.KeepAlive(transport)
			runtime.KeepAlive(client)
		})
	}
}

func registerThenRemoveCapturedListener(register func(func()) func()) weak.Pointer[[1024]byte] {
	payload := new([1024]byte)
	pointer := weak.Make(payload)
	remove := register(func() { runtime.KeepAlive(payload) })
	remove()
	return pointer
}
