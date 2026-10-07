package coding

import (
	"context"
	"errors"
	"testing"
)

// D200: an already canceled read must not launch any clipboard helper.
func TestClipboardReadContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	text, err := ReadClipboardTextContext(ctx)
	if text != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("read = (%q,%v), want canceled empty result", text, err)
	}
}

// D200: cancellation must be observed before command launch or OSC 52 fallback.
func TestClipboardCopyContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	t.Setenv("SSH_CONNECTION", "remote")
	if err := CopyTextToClipboardContext(ctx, "must not be emitted"); !errors.Is(err, context.Canceled) {
		t.Fatalf("copy error = %v, want context.Canceled", err)
	}
}
