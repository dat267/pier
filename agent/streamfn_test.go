package agent

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// TestDefaultStreamFn covers stream-fn.ts: the fallback is unset by default,
// round-trips a configured function, and clears back to unset.
func TestDefaultStreamFn(t *testing.T) {
	previous, hadPrevious := defaultStreamFn, defaultStreamFn != nil
	t.Cleanup(func() {
		if hadPrevious {
			SetDefaultStreamFn(previous)
		} else {
			SetDefaultStreamFn(nil)
		}
	})
	SetDefaultStreamFn(nil)
	if _, err := GetDefaultStreamFn(); err == nil || !strings.Contains(err.Error(), "No default stream function configured") {
		t.Fatalf("unset err = %v", err)
	}

	called := false
	streamFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		called = true
		return ai.NewAssistantMessageEventStream()
	}
	SetDefaultStreamFn(streamFn)
	got, err := GetDefaultStreamFn()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("default stream function is nil after being set")
	}
	_ = got(nil, ai.TranscriptContext{}, nil)
	if !called {
		t.Fatal("the configured stream function was not returned")
	}

	SetDefaultStreamFn(nil)
	if _, err := GetDefaultStreamFn(); err == nil {
		t.Fatal("clearing the default must leave it unset")
	}
}
