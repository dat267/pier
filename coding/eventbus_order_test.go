package coding

import (
	"fmt"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) core/event-bus.ts wraps a Node EventEmitter,
// which invokes the listeners of one event in subscription order. The port
// kept its subscribers in a map, so the same channel dispatched its handlers in
// Go's randomized map order (A428).
func TestEventBusDispatchesInSubscriptionOrder(t *testing.T) {
	bus := CreateEventBus()
	var order []string
	for index := range 5 {
		name := fmt.Sprintf("handler-%d", index)
		bus.On("channel", func(any) { order = append(order, name) })
	}
	// Map iteration is randomized per dispatch, so a few emits make a
	// regression certain without depending on a single ordering draw.
	for range 10 {
		order = order[:0]
		bus.Emit("channel", nil)
		want := "handler-0,handler-1,handler-2,handler-3,handler-4"
		if got := strings.Join(order, ","); got != want {
			t.Fatalf("dispatch order = %s; want subscription order %s", got, want)
		}
	}
}

// Unsubscribing keeps the remaining handlers in their subscription order.
func TestEventBusUnsubscribeKeepsOrder(t *testing.T) {
	bus := CreateEventBus()
	var order []string
	off := bus.On("channel", func(any) { order = append(order, "first") })
	bus.On("channel", func(any) { order = append(order, "second") })
	bus.On("channel", func(any) { order = append(order, "third") })
	off()
	off() // idempotent
	bus.Emit("channel", nil)
	if got, want := strings.Join(order, ","), "second,third"; got != want {
		t.Fatalf("dispatch order = %s; want %s", got, want)
	}
}
