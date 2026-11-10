package interactive

import (
	"testing"
	"time"
)

// An empty fork selector auto-cancels after upstream's 100ms timer
// (components/user-message-selector.ts, v1.5.0 f9a26365), and the cancel runs
// on the UI loop: the timer goroutine only posts it. With no loop to post to
// there is no timer at all, so onCancel never runs off the loop.
func TestEmptyForkSelectorCancelsOnlyThroughTheLoop(t *testing.T) {
	InitTheme("dark", false)
	delays := make(chan int, 2)
	original := delayMillis
	delayMillis = func(ms int) { delays <- ms }
	t.Cleanup(func() { delayMillis = original })

	cancels := 0
	posted := make(chan func(), 1)
	NewUserMessageSelectorComponent(nil, nil, func() { cancels++ }, "", func(fn func()) { posted <- fn })
	select {
	case ms := <-delays:
		if ms != 100 {
			t.Fatalf("delay = %dms, want 100", ms)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the empty selector scheduled no auto-cancel")
	}
	var fn func()
	select {
	case fn = <-posted:
	case <-time.After(5 * time.Second):
		t.Fatal("the auto-cancel was not posted")
	}
	if cancels != 0 {
		t.Fatal("onCancel ran on the timer goroutine")
	}
	fn()
	if cancels != 1 {
		t.Fatalf("cancels = %d after the posted callback ran, want 1", cancels)
	}

	NewUserMessageSelectorComponent(nil, nil, func() { cancels++ }, "", nil)
	select {
	case <-delays:
		t.Fatal("a selector with no loop scheduled an off-loop auto-cancel")
	case <-time.After(50 * time.Millisecond):
	}
}
