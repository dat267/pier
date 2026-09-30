package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestTerminalColorsParsing covers the OSC 11 / color-scheme parsers.
func TestTerminalColorsParsing(t *testing.T) {
	// OSC 11 responses.
	if !IsOsc11BackgroundColorResponse("\x1b]11;#ffffff\x07") {
		t.Fatal("BEL-terminated response not detected")
	}
	if !IsOsc11BackgroundColorResponse("\x1b]11;rgb:00/00/00\x1b\\") {
		t.Fatal("ST-terminated response not detected")
	}
	if IsOsc11BackgroundColorResponse("#ffffff") {
		t.Fatal("plain hex detected as a response")
	}

	color, ok := ParseOsc11BackgroundColor("\x1b]11;#102030\x07")
	if !ok || color.R != 0x10 || color.G != 0x20 || color.B != 0x30 {
		t.Fatalf("hex color = %+v ok=%v", color, ok)
	}
	color, ok = ParseOsc11BackgroundColor("\x1b]11;rgb:ff/80/00\x07")
	if !ok || color.R != 255 || color.G != 128 || color.B != 0 {
		t.Fatalf("rgb color = %+v ok=%v", color, ok)
	}
	color, ok = ParseOsc11BackgroundColor("\x1b]11;rgba:ffff/0000/0000\x07")
	if !ok || color.R != 255 || color.G != 0 || color.B != 0 {
		t.Fatalf("12-bit color = %+v ok=%v", color, ok)
	}
	if _, ok := ParseOsc11BackgroundColor("\x1b]11;not-a-color\x07"); ok {
		t.Fatal("invalid color parsed")
	}
	if _, ok := ParseOsc11BackgroundColor("nope"); ok {
		t.Fatal("non-response parsed")
	}

	// Color-scheme reports.
	if scheme, ok := ParseTerminalColorSchemeReport("\x1b[?997;1n"); !ok || scheme != TerminalColorSchemeDark {
		t.Fatalf("dark scheme = %q ok=%v", scheme, ok)
	}
	if scheme, ok := ParseTerminalColorSchemeReport("\x1b[?997;2n"); !ok || scheme != TerminalColorSchemeLight {
		t.Fatalf("light scheme = %q ok=%v", scheme, ok)
	}
	if _, ok := ParseTerminalColorSchemeReport("\x1b[?997;3n"); ok {
		t.Fatal("invalid scheme parsed")
	}
}

// TestRendererTerminalQueries covers the renderer query round-trips.
func TestRendererTerminalQueries(t *testing.T) {
	terminal := &fakeTerminal{width: 80, height: 24}
	renderer := NewRenderer(terminal)

	// OSC 11: the reply is consumed by HandleTerminalInput.
	results := make(chan struct {
		color RgbColor
		ok    bool
	}, 1)
	go func() {
		color, ok := renderer.QueryTerminalBackgroundColor(2000)
		results <- struct {
			color RgbColor
			ok    bool
		}{color, ok}
	}()
	waitForWrite(t, terminal, "\x1b]11;?")
	renderer.HandleTerminalInput("\x1b]11;#0a0b0c\x07")
	result := <-results
	if !result.ok || result.color.R != 10 || result.color.G != 11 || result.color.B != 12 {
		t.Fatalf("background = %+v ok=%v", result.color, result.ok)
	}

	// A timeout returns ok=false.
	if _, ok := renderer.QueryTerminalBackgroundColor(1); ok {
		t.Fatal("query without a reply should time out")
	}

	// Color scheme: a listener is notified by the report consumer.
	schemes := make(chan TerminalColorScheme, 1)
	unsubscribe := renderer.OnTerminalColorSchemeChange(func(scheme TerminalColorScheme) { schemes <- scheme })
	renderer.HandleTerminalInput("\x1b[?997;2n")
	if scheme := <-schemes; scheme != TerminalColorSchemeLight {
		t.Fatalf("scheme = %q", scheme)
	}
	unsubscribe()
	renderer.HandleTerminalInput("\x1b[?997;1n")
	select {
	case scheme := <-schemes:
		t.Fatalf("unexpected scheme after unsubscribe: %q", scheme)
	default:
	}

	// The color-scheme query round-trips.
	schemeResults := make(chan TerminalColorScheme, 1)
	go func() {
		scheme, ok := renderer.QueryTerminalColorScheme(2000)
		if ok {
			schemeResults <- scheme
		}
	}()
	waitForWrite(t, terminal, "\x1b[?996n")
	renderer.HandleTerminalInput("\x1b[?997;1n")
	if scheme := <-schemeResults; scheme != TerminalColorSchemeDark {
		t.Fatalf("queried scheme = %q", scheme)
	}

	// Notifications toggle writes the mode sequences and Stop disables them.
	// The renderer must be started: before Start the toggle only flips the flag
	// (Start replays it once raw mode is active).
	renderer.Start()
	renderer.SetTerminalColorSchemeNotifications(true)
	renderer.SetTerminalColorSchemeNotifications(false)
	renderer.SetTerminalColorSchemeNotifications(true)
	renderer.Stop(TuiStopOptions{})
	joined := terminal.joinedWrites()
	if !strings.Contains(joined, "\x1b[?2031h") || !strings.Contains(joined, "\x1b[?2031l") {
		t.Fatalf("writes = %q", joined)
	}
}

// waitForWrite waits until the terminal has seen a write containing the
// substring.
func waitForWrite(t *testing.T, terminal *fakeTerminal, substring string) {
	t.Helper()
	for i := 0; i < 20000; i++ {
		if terminal.hasWrite(substring) {
			return
		}
		timeSleepMicro()
	}
	t.Fatalf("terminal writes = %d, missing %q", terminal.writeCount(), substring)
}

func timeSleepMicro() {
	time.Sleep(50 * time.Microsecond)
}

// TestRendererBackgroundProbeIsNonBlocking covers the listener-driven OSC 11
// probe: RequestTerminalBackgroundColor returns immediately and the reply is
// delivered to listeners with no pending blocking query. A reply must never leak
// to the input listeners (which would type garbage into the editor).
func TestRendererBackgroundProbeIsNonBlocking(t *testing.T) {
	terminal := &fakeTerminal{width: 80, height: 24}
	renderer := NewRenderer(terminal)
	got := make(chan RgbColor, 1)
	unsubscribe := renderer.OnTerminalBackgroundColorChange(func(color RgbColor) { got <- color })

	renderer.RequestTerminalBackgroundColor()
	waitForWrite(t, terminal, "\x1b]11;?")
	renderer.HandleTerminalInput("\x1b]11;#112233\x07")
	select {
	case color := <-got:
		if color.R != 0x11 || color.G != 0x22 || color.B != 0x33 {
			t.Fatalf("color = %+v", color)
		}
	case <-time.After(time.Second):
		t.Fatal("background listener not notified")
	}

	var leaked []string
	renderer.AddInputListener(func(data string) TuiInputListenerResult {
		leaked = append(leaked, data)
		return TuiInputListenerResult{}
	})
	renderer.HandleTerminalInput("\x1b]11;#445566\x07")
	if len(leaked) != 0 {
		t.Fatalf("OSC 11 reply leaked to input listeners: %v", leaked)
	}
	select {
	case <-got:
	default:
	}

	unsubscribe()
	renderer.HandleTerminalInput("\x1b]11;#778899\x07")
	select {
	case color := <-got:
		t.Fatalf("listener called after unsubscribe: %+v", color)
	default:
	}
}

// TestRendererDefersColorSchemeNotificationsUntilStart pins that a notification
// toggle requested before the terminal starts is not written (it would be echoed
// into the input stream over SSH) and is replayed by Start.
func TestRendererDefersColorSchemeNotificationsUntilStart(t *testing.T) {
	terminal := &fakeTerminal{width: 80, height: 24}
	renderer := NewRenderer(terminal)
	renderer.SetTerminalColorSchemeNotifications(true)
	if terminal.hasWrite("\x1b[?2031h") {
		t.Fatal("notification written before Start")
	}
	renderer.Start()
	if !terminal.hasWrite("\x1b[?2031h") {
		t.Fatal("notification not replayed by Start")
	}
	renderer.Stop(TuiStopOptions{})
}

// TestParseOscColorResponse ports the parseOscColorResponse cases from upstream
// terminal-colors.test.ts (bf8e4b953).
func TestParseOscColorResponse(t *testing.T) {
	response, ok := ParseOscColorResponse("\x1b]10;rgb:ffff/ffff/ffff\x07")
	if !ok || !response.Target.Foreground || !response.HasRGB || response.RGB != (RgbColor{R: 255, G: 255, B: 255}) {
		t.Fatalf("foreground reply = %+v ok=%v", response, ok)
	}
	response, ok = ParseOscColorResponse("\x1b]4;13;#ff0080\x1b\\")
	if !ok || response.Target.Foreground || response.Target.Background || response.Target.Index != 13 || !response.HasRGB || response.RGB != (RgbColor{R: 255, G: 0, B: 128}) {
		t.Fatalf("palette reply = %+v ok=%v", response, ok)
	}
	response, ok = ParseOscColorResponse("\x1b]4;1;bogus\x07")
	if !ok || response.Target.Index != 1 || response.HasRGB {
		t.Fatalf("unparsable reply = %+v ok=%v", response, ok)
	}
	if _, ok := ParseOscColorResponse("\x1b]12;#ffffff\x07"); ok {
		t.Fatal("OSC 12 parsed as a color response")
	}
}

// waitForWritesAfter waits until the terminal has recorded more than before
// writes.
func waitForWritesAfter(t *testing.T, terminal *fakeTerminal, before int) {
	t.Helper()
	for i := 0; i < 20000; i++ {
		if terminal.writeCount() > before {
			return
		}
		timeSleepMicro()
	}
	t.Fatal("no new terminal write")
}

// TestRendererQueryTerminalColorsResolvesOnAllReplies ports the first
// queryTerminalColors case: one write, all replies, no DA1 needed.
func TestRendererQueryTerminalColorsResolvesOnAllReplies(t *testing.T) {
	terminal := &fakeTerminal{width: 80, height: 24}
	renderer := NewRenderer(terminal)
	results := make(chan TerminalColors, 1)
	before := terminal.writeCount()
	go func() { results <- renderer.QueryTerminalColors(2000, nil) }()
	waitForWritesAfter(t, terminal, before)
	joined := terminal.joinedWrites()
	if !strings.HasPrefix(joined, "\x1b]10;?\x07\x1b]11;?\x07\x1b]4;0;?\x07") || !strings.HasSuffix(joined, "\x1b[c") {
		t.Fatalf("query write = %q", joined)
	}
	renderer.HandleTerminalInput("\x1b]10;#ffffff\x07")
	renderer.HandleTerminalInput("\x1b]11;rgb:0000/0000/0000\x1b\\")
	for index := 0; index < 16; index++ {
		renderer.HandleTerminalInput(fmt.Sprintf("\x1b]4;%d;#000000\x07", index))
	}
	got := <-results
	if got.Foreground == nil || *got.Foreground != (RgbColor{R: 255, G: 255, B: 255}) {
		t.Fatalf("foreground = %+v", got.Foreground)
	}
	if got.Background == nil || *got.Background != (RgbColor{}) {
		t.Fatalf("background = %+v", got.Background)
	}
	if len(got.Palette) != 16 {
		t.Fatalf("palette = %d", len(got.Palette))
	}
}

// TestRendererQueryTerminalColorsResolvesOnDA1 ports the second case: an
// incomplete palette is dropped and DA1 resolves each query in order.
func TestRendererQueryTerminalColorsResolvesOnDA1(t *testing.T) {
	terminal := &fakeTerminal{width: 80, height: 24}
	renderer := NewRenderer(terminal)
	run := func(action func()) TerminalColors {
		results := make(chan TerminalColors, 1)
		before := terminal.writeCount()
		go func() { results <- renderer.QueryTerminalColors(2000, nil) }()
		waitForWritesAfter(t, terminal, before)
		action()
		return <-results
	}
	first := run(func() {
		renderer.HandleTerminalInput("\x1b]11;#000000\x07")
		for index := 0; index < 8; index++ {
			renderer.HandleTerminalInput(fmt.Sprintf("\x1b]4;%d;#000000\x07", index))
		}
		renderer.HandleTerminalInput("\x1b[?62;22c")
	})
	if first.Foreground != nil || first.Background == nil || *first.Background != (RgbColor{}) || first.Palette != nil {
		t.Fatalf("first = %+v", first)
	}
	second := run(func() { renderer.HandleTerminalInput("\x1b[?62;22c") })
	if second.Foreground != nil || second.Background != nil || second.Palette != nil {
		t.Fatalf("second = %+v", second)
	}
}

// TestRendererQueryTerminalColorsLateReplies ports the timeout case: late
// replies reach onLateReply until DA1.
func TestRendererQueryTerminalColorsLateReplies(t *testing.T) {
	terminal := &fakeTerminal{width: 80, height: 24}
	renderer := NewRenderer(terminal)
	late := make(chan TerminalColors, 1)
	results := make(chan TerminalColors, 1)
	before := terminal.writeCount()
	go func() {
		results <- renderer.QueryTerminalColors(1, func(colors TerminalColors) { late <- colors })
	}()
	waitForWritesAfter(t, terminal, before)
	select {
	case got := <-results:
		if got.Background != nil {
			t.Fatalf("timeout result = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("query did not time out")
	}
	renderer.HandleTerminalInput("\x1b]11;#ffffff\x07")
	renderer.HandleTerminalInput("\x1b[?62;22c")
	select {
	case colors := <-late:
		if colors.Background == nil || *colors.Background != (RgbColor{R: 255, G: 255, B: 255}) {
			t.Fatalf("late = %+v", colors)
		}
	case <-time.After(time.Second):
		t.Fatal("no late reply")
	}
}
