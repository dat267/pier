package tui

import (
	"testing"
)

// selectionText runs a drag from the first cell of the first row to the end of
// the last row and returns the copied text.
func selectionText(t *testing.T, width int, lines []string) string {
	t.Helper()
	terminal := &recordingTerminal{width: width, height: 6}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	screen.AddChild(&scriptedComponent{lines: lines})
	screen.Start()
	screen.RenderNow(false)
	screen.SetCopyOnSelect(false)

	screen.HandleTerminalInput("\x1b[<0;1;1M")
	screen.HandleTerminalInput("\x1b[<32;" + itoa(width) + ";" + itoa(len(lines)) + "M")
	screen.HandleTerminalInput("\x1b[<0;" + itoa(width) + ";" + itoa(len(lines)) + "m")

	text, ok := screen.getActiveSelectionTextLocked()
	if !ok {
		t.Fatal("no selection text")
	}
	return text
}

// D472: a row that runs to the row width was wrapped by the renderer, so the
// copy joins it with the next row. A shorter row ended with a newline.
func TestSelectionJoinsSoftWrappedRows(t *testing.T) {
	got := selectionText(t, 10, []string{"abcdefghij", "klm"})
	if want := "abcdefghijklm"; got != want {
		t.Fatalf("wrapped copy = %q, want %q", got, want)
	}
}

func TestSelectionKeepsHardLineBreaks(t *testing.T) {
	got := selectionText(t, 10, []string{"abc", "klm"})
	if want := "abc\nklm"; got != want {
		t.Fatalf("copy = %q, want %q", got, want)
	}
}

// The continuation row repeats the block's indent; the copy covers only the
// rows' text, so the joined line carries no indentation.
func TestSelectionJoinsWrappedRowsWithoutTheirIndent(t *testing.T) {
	got := selectionText(t, 10, []string{"  abcdefgh", "  klm"})
	if want := "abcdefghklm"; got != want {
		t.Fatalf("indented wrapped copy = %q, want %q", got, want)
	}
}

// The copied text matches the highlight: a drag from the margin copies the rows'
// text without their indentation.
func TestSelectionCopySkipsLeadingPadding(t *testing.T) {
	got := selectionText(t, 20, []string{"  alpha", "  beta"})
	if want := "alpha\nbeta"; got != want {
		t.Fatalf("copy = %q, want %q", got, want)
	}
}

// A drag that starts inside the text copies from there.
func TestSelectionCopyHonorsAStartInsideTheText(t *testing.T) {
	terminal := &recordingTerminal{width: 20, height: 6}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	screen.AddChild(&scriptedComponent{lines: []string{"  alpha"}})
	screen.Start()
	screen.RenderNow(false)
	screen.SetCopyOnSelect(false)

	screen.HandleTerminalInput("\x1b[<0;3;1M")
	screen.HandleTerminalInput("\x1b[<32;20;1M")
	screen.HandleTerminalInput("\x1b[<0;20;1m")

	got, ok := screen.getActiveSelectionTextLocked()
	if !ok {
		t.Fatal("no selection text")
	}
	if want := "alpha"; got != want {
		t.Fatalf("copy = %q, want %q", got, want)
	}
}

// The transcript lives in a scroll view: the wrap width there is the content
// width the view rendered its child at.
func TestSelectionJoinsSoftWrappedRowsInAScrollView(t *testing.T) {
	terminal := &recordingTerminal{width: 10, height: 6}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	view := NewScrollView(NewText("abcdefghijklm", 0, 0, nil), ScrollViewOptions{Primary: true})
	screen.AddChild(view)
	screen.Start()
	screen.RenderNow(false)
	screen.SetCopyOnSelect(false)

	screen.HandleTerminalInput("\x1b[<0;1;1M")
	screen.HandleTerminalInput("\x1b[<32;10;2M")
	screen.HandleTerminalInput("\x1b[<0;10;2m")

	got, ok := screen.getActiveSelectionTextLocked()
	if !ok {
		t.Fatal("no selection text")
	}
	if want := "abcdefghijklm"; got != want {
		t.Fatalf("scroll view copy = %q, want %q", got, want)
	}
}

// narrowComponent renders every line padded to one column less than it is
// given, the way the transcript's output padding does.
type narrowComponent struct{ lines []string }

func (c *narrowComponent) Render(width int) []string {
	out := make([]string, 0, len(c.lines))
	for _, line := range c.lines {
		for VisibleWidth(line) < width-1 {
			line += " "
		}
		out = append(out, line)
	}
	return out
}

func (c *narrowComponent) Invalidate() {}

// The transcript pads its messages, so a wrapped row stops a column short of
// the row width. The wrap width is the selection's widest right edge.
func TestSelectionJoinsWrappedRowsWithOutputPadding(t *testing.T) {
	terminal := &recordingTerminal{width: 10, height: 6}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	view := NewScrollView(&narrowComponent{lines: []string{"abcdefghi", "klm"}}, ScrollViewOptions{Primary: true})
	screen.AddChild(view)
	screen.Start()
	screen.RenderNow(false)
	screen.SetCopyOnSelect(false)

	screen.HandleTerminalInput("\x1b[<0;1;1M")
	screen.HandleTerminalInput("\x1b[<32;10;2M")
	screen.HandleTerminalInput("\x1b[<0;10;2m")

	got, ok := screen.getActiveSelectionTextLocked()
	if !ok {
		t.Fatal("no selection text")
	}
	if want := "abcdefghiklm"; got != want {
		t.Fatalf("padded wrapped copy = %q, want %q", got, want)
	}
}

// Indentation beyond the shared margin is content and stays in the copy.
func TestSelectionCopyKeepsLegitimateIndent(t *testing.T) {
	got := selectionText(t, 20, []string{"FAIL: x", "     y"})
	if want := "FAIL: x\n     y"; got != want {
		t.Fatalf("copy = %q, want %q", got, want)
	}
}

// wrappedProseCopy renders text through the real wrapper in a scroll view and
// returns what a select-all copies.
func wrappedProseCopy(t *testing.T, width int, text string) string {
	t.Helper()
	terminal := &recordingTerminal{width: width, height: 6}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	view := NewScrollView(NewText(text, 0, 0, nil), ScrollViewOptions{Primary: true})
	screen.AddChild(view)
	screen.Start()
	screen.RenderNow(false)
	screen.SetCopyOnSelect(false)

	rows := len(screen.previousScreen)
	screen.HandleTerminalInput("\x1b[<0;1;1M")
	screen.HandleTerminalInput("\x1b[<32;" + itoa(width) + ";" + itoa(rows) + "M")
	screen.HandleTerminalInput("\x1b[<0;" + itoa(width) + ";" + itoa(rows) + "m")

	got, ok := screen.getActiveSelectionTextLocked()
	if !ok {
		t.Fatal("no selection text")
	}
	return got
}

// A wrapped paragraph copies as one line, with the spaces the wrapper dropped
// put back.
func TestSelectionJoinsWrappedProseWithSpaces(t *testing.T) {
	text := "the quick brown fox jumps over the lazy dog"
	if got := wrappedProseCopy(t, 20, text); got != text {
		t.Fatalf("wrapped prose copy = %q, want %q", got, text)
	}
}

// A line the content broke keeps its break.
func TestSelectionKeepsProseLineBreaks(t *testing.T) {
	if got := wrappedProseCopy(t, 20, "abc\ndefgh"); got != "abc\ndefgh" {
		t.Fatalf("copy = %q, want the hard break kept", got)
	}
}

// tripleClick presses and releases on a cell three times, which the click
// counter turns into a line selection.
func tripleClick(screen *AltScreen, column, row int) {
	press := "\x1b[<0;" + itoa(column) + ";" + itoa(row) + "M"
	release := "\x1b[<0;" + itoa(column) + ";" + itoa(row) + "m"
	for range 3 {
		screen.HandleTerminalInput(press)
		screen.HandleTerminalInput(release)
	}
}

// A triple click selects the whole line the content wrote, including the rows
// the renderer wrapped it into (D472).
func TestTripleClickSelectsAWrappedLine(t *testing.T) {
	text := "the quick brown fox jumps over the lazy dog"
	terminal := &recordingTerminal{width: 20, height: 6}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	screen.AddChild(NewScrollView(NewText(text, 0, 0, nil), ScrollViewOptions{Primary: true}))
	screen.Start()
	screen.RenderNow(false)
	screen.SetCopyOnSelect(false)

	tripleClick(screen, 3, 1)

	got, ok := screen.getActiveSelectionTextLocked()
	if !ok {
		t.Fatal("no selection text")
	}
	if got != text {
		t.Fatalf("triple click copied %q, want %q", got, text)
	}
}

// A line the content broke is one row, so a triple click stays on it.
func TestTripleClickSelectsOneHardLine(t *testing.T) {
	terminal := &recordingTerminal{width: 20, height: 6}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	screen.AddChild(NewScrollView(NewText("abc\ndefgh", 0, 0, nil), ScrollViewOptions{Primary: true}))
	screen.Start()
	screen.RenderNow(false)
	screen.SetCopyOnSelect(false)

	tripleClick(screen, 1, 1)

	got, ok := screen.getActiveSelectionTextLocked()
	if !ok {
		t.Fatal("no selection text")
	}
	if got != "abc" {
		t.Fatalf("triple click copied %q, want %q", got, "abc")
	}
}
