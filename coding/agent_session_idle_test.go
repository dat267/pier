package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.5.0 f9a26365 core/agent-session.ts waitForIdle registers
// its promise synchronously in the single-threaded reference. In Go a run can
// finish between the idle check and waiter registration; registering first and
// rechecking keeps that completion from being lost.
func TestIdleWaitRegistrationAfterCompletionDoesNotBlock(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000}
	session := newPromptSession(t, model, unreachableStreamFn)

	// A completion that already happened before registration must report idle
	// instead of returning a channel no completion will ever close.
	if wait := session.registerIdleWait(); wait != nil {
		t.Fatal("idle registration returned a wait channel")
	}

	// While a run is active the same registration returns the channel that
	// completion resolves.
	state := session.prompt()
	state.mu.Lock()
	state.runActive = true
	state.mu.Unlock()
	wait := session.registerIdleWait()
	if wait == nil {
		t.Fatal("active registration reported idle")
	}
	state.mu.Lock()
	state.runActive = false
	state.mu.Unlock()
	session.resolveIdleWaitIfIdle()
	select {
	case <-wait:
	default:
		t.Fatal("completion did not resolve the registered waiter")
	}
}
