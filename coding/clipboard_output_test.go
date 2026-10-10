package coding

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// D202: interactive OSC 52 fallback uses an injected, cancellation-aware
// terminal sink, not a second direct writer to the process's stdout.
func TestClipboardOSC52RejectsOversizedPayload(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SSH_CONNECTION", "remote")
	t.Setenv("TERMUX_VERSION", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	writes := 0
	err := CopyTextToClipboardWithOSC52(context.Background(), strings.Repeat("x", MaxOSC52EncodedLength+1), func(context.Context, string) error {
		writes++
		return nil
	})
	if err == nil || writes != 0 {
		t.Fatalf("oversized copy err=%v writes=%d", err, writes)
	}
}

func TestClipboardOSC52UsesInjectedContextWriter(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SSH_CONNECTION", "remote")
	t.Setenv("TERMUX_VERSION", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	sentinel := errors.New("output canceled")
	var packet string
	err := CopyTextToClipboardWithOSC52(context.Background(), "hello", func(ctx context.Context, data string) error { packet = data; return sentinel })
	if !errors.Is(err, sentinel) || packet != "\x1b]52;c;aGVsbG8=\x07" {
		t.Fatalf("fallback=(%q,%v)", packet, err)
	}
}
