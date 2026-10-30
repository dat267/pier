package mcp

import (
	"encoding/json"
	"testing"

	"github.com/dat267/pier/mcp/protocol"
)

// D312: pi v1.4.0 packages/mcp/src/client.ts iterates its live Set, so a
// listener removed by an earlier callback is skipped for the current message.
// Go's lock-safe snapshot intentionally keeps already-admitted listeners.
func TestClientNotificationUnsubscribeDuringDispatchUsesSnapshot(t *testing.T) {
	client := NewClient(ClientOptions{})
	var got []string
	var removeSecond func()
	client.OnNotification("test/event", func(json.RawMessage) {
		got = append(got, "first")
		removeSecond()
	})
	removeSecond = client.OnNotification("test/event", func(json.RawMessage) {
		got = append(got, "second")
	})

	client.handleNotification(&protocol.JsonRpcNotification{Method: "test/event"})
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("listeners called = %v, want [first second] for the admitted snapshot", got)
	}
}
