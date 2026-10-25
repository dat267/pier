package rpcclient

import (
	"strings"
	"testing"
)

// pi v1.4.0 modes/rpc/rpc-client.ts iterates a snapshot of a Set, so listeners
// run in registration order. Go map iteration would randomize that order, which
// is observable for any listener with side effects.
func TestClientListenersRunInRegistrationOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess round trip")
	}
	client := startFakeClient(t, nil)
	var order []string
	client.OnEvent(func(Event) { order = append(order, "first") })
	client.OnEvent(func(Event) { order = append(order, "second") })
	for range 25 {
		order = order[:0]
		if _, err := client.State(); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(order, ","); got != "first,second" {
			t.Fatalf("listener order = %q, want first,second", got)
		}
	}
}
