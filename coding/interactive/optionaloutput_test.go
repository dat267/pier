package interactive

import (
	"context"
	"testing"
)

type optionalClipboardTerminal struct {
	fakeRendererTerminal
	optional string
}

func (t *optionalClipboardTerminal) WriteOptionalContext(ctx context.Context, data string) error {
	t.optional = data
	return nil
}

// D202: the CLI fallback must use optional terminal admission, not raw stdout.
func TestClipboardFallbackUsesOptionalTerminalSink(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SSH_CONNECTION", "remote")
	t.Setenv("TERMUX_VERSION", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	base, cleanup := newTestAppB(t)
	defer cleanup()
	options := base.options
	terminal := &optionalClipboardTerminal{fakeRendererTerminal: fakeRendererTerminal{width: 80, height: 24}}
	options.Terminal = terminal
	app := NewApp(options)
	defer app.Close()
	if err := app.copyText(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if terminal.optional != "\x1b]52;c;aGVsbG8=\x07" {
		t.Fatal("OSC 52 bypassed optional terminal admission")
	}
}
