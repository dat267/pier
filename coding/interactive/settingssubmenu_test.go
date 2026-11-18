package interactive

import (
	"context"
	"github.com/dat267/pier/tui"
	"strings"
	"testing"
	"time"
)

// Reference: pi v1.5.2 modes/interactive/components/settings-submenu.ts
// handleInput treats tui.select.pageUp and tui.select.pageDown as navigation
// keys of a searchable submenu, so a page key moves the list instead of
// reaching the filter input.
func TestSearchableSubmenuPagesWithoutTypingIntoTheQuery(t *testing.T) {
	InitTheme("dark", false)
	options := make([]tui.SelectItem, 12)
	for index := range options {
		options[index] = tui.SelectItem{Value: itoa(index), Label: "Item " + itoa(index)}
	}
	submenu := NewSelectSubmenu("Title", "description", options, "", nil, nil, nil,
		SelectSubmenuOptions{Searchable: true})

	submenu.HandleInput("\x1b[6~") // pageDown
	if index := submenu.GetSelectList().SelectedIndex(); index != 10 {
		t.Fatalf("PageDown selected %d, want the second page start 10", index)
	}
	if value := submenu.searchInput.Value(); value != "" {
		t.Fatalf("query = %q, want the page key kept out of the filter", value)
	}
	submenu.HandleInput("\x1b[5~") // pageUp
	if index := submenu.GetSelectList().SelectedIndex(); index != 0 {
		t.Fatalf("PageUp selected %d, want 0", index)
	}
}

// TestDisplayOptionsSingleOwner pins the display-settings seam: output padding,
// thinking-block visibility, the hidden-thinking label and tool-output
// expansion had a copy on the transcript, the event dispatcher, the run wiring,
// the trust wiring and the UI state, synced by hand in app.go. Two copies were
// never assigned (TranscriptRenderer/EventDispatcher.ToolOutputExpanded) and
// RunWiring.OutputPad went stale on /reload. One DisplayOptions value is now
// shared by every holder and mutated in one place.
func TestDisplayOptionsSingleOwner(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	app.Init(context.Background())

	if app.display == nil {
		t.Fatal("app has no display options")
	}
	holders := map[string]*DisplayOptions{
		"transcript": app.transcript.Display,
		"events":     app.events.Display,
		"runner":     app.runner.Display,
		"trust":      app.trust.Display,
		"uiState":    app.uiState.Display,
	}
	for name, got := range holders {
		if got != app.display {
			t.Fatalf("%s does not share the app display options", name)
		}
	}

	// ctrl+o flips the single owner, so components created afterwards inherit
	// the expansion state (the dead copies kept them collapsed).
	app.key.OnToolsExpand()
	if !app.display.ToolOutputExpanded {
		t.Fatal("tool expansion did not reach the shared display options")
	}

	// /reload re-reads the padding into the single owner, including the run
	// wiring's error lines. OutputPad clamps to 0 or 1 (GetOutputPad).
	app.settings.SetOutputPad(0)
	app.applyReloadedSettings()
	if app.display.OutputPad != 0 {
		t.Fatalf("output pad = %d, want 0 after reload", app.display.OutputPad)
	}
}

// TestCompactCommandReportsErrorAndClearsIndicator drives /compact on an empty
// session: the compaction fails ("Nothing to compact"), the compaction_end
// event must clear the status indicator and the error must surface in the
// chat. Regression for the stuck compaction spinner.
//
// The command is invoked before the loop starts, so its events are queued and
// drained by the loop (stage 1: producers only enqueue). Running the command
// while the loop applies events would exercise the input race that
// stage 3 removes, not this behaviour.
func TestCompactCommandReportsErrorAndClearsIndicator(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	app.Init(ctx)
	app.commands.HandleCompactCommand(ctx, "")

	done := make(chan struct{})
	go func() {
		defer close(done)
		app.Run(ctx)
	}()

	// Container reads are lock-guarded, so the assertion can run while the
	// loop drains the queued events.
	waitForConditionWithin(t, func() bool {
		return strings.Contains(renderAppChat(app), "Compaction failed")
	}, 6*time.Second)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run loop did not exit within 2s of cancellation")
	}
	// The loop has stopped, so the indicator state is safe to read.
	if app.events.UIState != nil && app.events.UIState.ActiveStatusIndicator != nil {
		t.Fatalf("compaction status indicator still active after /compact: %T",
			app.events.UIState.ActiveStatusIndicator)
	}
}

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
	terminal := &optionalClipboardTerminal{width: 80, height: 24}
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
