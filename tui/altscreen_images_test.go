package tui

import (
	"strings"
	"testing"
)

func kittyImageLine(t *testing.T, imageID int, payload string) string {
	t.Helper()
	RegisterKittyImageMetadata(KittyImageMetadata{ImageID: imageID, Columns: 2, Rows: 3, WidthPx: 100, HeightPx: 100})
	move := false
	return EncodeKitty(payload, KittyEncodeOptions{
		Columns: 2, HasColumns: true, Rows: 3, HasRows: true, ImageID: imageID, HasImageID: true, MoveCursor: &move,
	})
}

// pi v1.4.0 prepareKittyScreen: the first frame keeps the transmission, and a
// later frame with the same generation reuses its placement.
func TestPrepareKittyScreenReusesUploadedTransmission(t *testing.T) {
	line := kittyImageLine(t, 101, "AAAA")
	screen := &AltScreen{}
	first, evicted := screen.prepareKittyScreen([]string{"text", line})
	if first[1] != line || evicted != "" {
		t.Fatalf("first frame = %q evicted = %q", first[1], evicted)
	}
	second, evicted := screen.prepareKittyScreen([]string{"text", line})
	placement, ok := GetKittyImagePlacement(line)
	if !ok {
		t.Fatal("no placement")
	}
	if second[1] != placement.ReplacementLine || evicted != "" {
		t.Fatalf("second frame = %q want %q", second[1], placement.ReplacementLine)
	}
	if len(screen.uploadedKittyImages) != 1 {
		t.Fatalf("cache = %d", len(screen.uploadedKittyImages))
	}
}

// Offscreen cached images are evicted (data freed) once the count exceeds the
// upstream limit.
func TestPrepareKittyScreenEvictsOffscreenBeyondLimits(t *testing.T) {
	screen := &AltScreen{}
	var lines []string
	for id := 200; id < 220; id++ {
		lines = append(lines, kittyImageLine(t, id, "AAAA"))
	}
	screen.prepareKittyScreen(lines)
	if len(screen.uploadedKittyImages) != 20 {
		t.Fatalf("cache = %d", len(screen.uploadedKittyImages))
	}
	// Keep only the newest image visible; the rest are offscreen, so the
	// oldest are evicted down to the limit.
	_, evicted := screen.prepareKittyScreen([]string{lines[19]})
	if evicted == "" {
		t.Fatal("expected evictions")
	}
	for id := range 3 {
		if !strings.Contains(evicted, DeleteKittyImage(200+id)) {
			t.Fatalf("missing eviction for %d: %q", 200+id, evicted)
		}
	}
	if len(screen.uploadedKittyImages) > maxCachedOffscreenKittyImages+1 {
		t.Fatalf("cache = %d", len(screen.uploadedKittyImages))
	}
}

// Some terminals store kitty images in the cells they cover (WezTerm, xterm.js's
// image addon in Orca), so clearing or writing a covered row erases that part of
// the image. pi v1.5.2 (b4afdddf) tui-alt-screen.ts therefore draws placements
// after every text row on every kitty terminal, not only on WezTerm.
func TestAltScreenDrawsKittyImagesLast(t *testing.T) {
	for _, terminal := range []struct {
		name string
		env  map[string]string
	}{
		{"wezterm", map[string]string{"WEZTERM_PANE": "1"}},
		{"orca-xterm-js", map[string]string{"TERM_PROGRAM": "Orca"}},
		{"kitty", map[string]string{"KITTY_WINDOW_ID": "1"}},
	} {
		t.Run(terminal.name, func(t *testing.T) {
			for name, value := range terminal.env {
				t.Setenv(name, value)
			}
			testAltScreenDrawsKittyImagesLast(t)
		})
	}
}

func testAltScreenDrawsKittyImagesLast(t *testing.T) {
	previous := GetTerminalCapabilities()
	previousCells := GetCellDimensions()
	t.Cleanup(func() {
		SetTerminalCapabilities(previous)
		SetCellDimensions(previousCells)
	})
	SetTerminalCapabilities(TerminalCapabilities{Images: ImageProtocolKitty, TrueColor: true})
	SetCellDimensions(CellDimensions{WidthPx: 10, HeightPx: 10})

	imageLine := kittyImageLine(t, 301, "AAAA")
	terminal := &recordingTerminal{width: 40, height: 6}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	component := &scriptedComponent{lines: []string{"alpha", imageLine, "beta"}}
	screen.AddChild(component)
	screen.Start()
	screen.RenderNow(false)
	out := terminal.takeWrites()

	transmission := strings.Index(out, "\x1b_Ga=T,f=100")
	alpha := strings.Index(out, "alpha")
	beta := strings.Index(out, "beta")
	if transmission < 0 || alpha < 0 || beta < 0 {
		t.Fatalf("frame missing content: %q", out)
	}
	if transmission < alpha || transmission < beta {
		t.Fatalf("image written before a text row: %q", out)
	}
}
