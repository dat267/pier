package interactive

import (
	"fmt"
	"github.com/dat267/pier/tui"
	"testing"
)

type resizeProbe struct {
	width int
	cold  *int
	text  string
}

func (p *resizeProbe) Invalidate()       {}
func (p *resizeProbe) Prepare(width int) { p.Render(width) }
func (p *resizeProbe) Render(width int) []string {
	if p.width != width {
		*p.cold++
		p.width = width
	}
	return []string{fmt.Sprintf("%s:%d", p.text, width)}
}

// D203: settled transcript width changes warm only a fixed number of direct
// children per render. The previous complete frame stays visible meanwhile.
func TestAppTranscriptResizeUsesBudgetedDocument(t *testing.T) {
	app, cleanup := newTestAppB(t)
	defer cleanup()
	cold := 0
	for i := range 130 {
		app.chat.AddChild(&resizeProbe{cold: &cold, text: fmt.Sprint(i)})
	}
	app.documentContainer.Render(80)
	cold = 0
	app.documentContainer.Render(81)
	if cold > 64 {
		t.Fatalf("app resize warmed %d children in one call, limit 64", cold)
	}
}

func TestTranscriptResizeRestartsForLatestWidthAndChildren(t *testing.T) {
	chat := &tui.Container{}
	cold := 0
	for i := range 130 {
		chat.AddChild(&resizeProbe{cold: &cold, text: fmt.Sprint(i)})
	}
	document := newResizeDocument(chat, func() {})
	document.Render(80)
	document.Render(81)
	document.Render(82)
	chat.AddChild(&resizeProbe{cold: &cold, text: "new"})
	for range 4 {
		document.Render(82)
	}
	lines := document.Render(82)
	if len(lines) != 131 || lines[0] != "0:82" || lines[130] != "new:82" {
		t.Fatal("superseded width or child snapshot was published")
	}
}

func TestTranscriptResizeYieldsBetweenWarmChunks(t *testing.T) {
	chat := &tui.Container{}
	cold := 0
	for i := range 130 {
		chat.AddChild(&resizeProbe{cold: &cold, text: fmt.Sprint(i)})
	}
	wakes := 0
	document := newResizeDocument(chat, func() { wakes++ })
	old := document.Render(80)
	parent := &tui.Container{SkipUnchangedChildren: true}
	parent.AddChild(document)
	parent.Render(80)
	cold = 0
	first := parent.Render(81)
	if cold > 64 {
		t.Fatalf("resize warmed %d children in one call, limit 64", cold)
	}
	if first[0] != old[0] || wakes == 0 {
		t.Fatal("resize did not retain complete frame and schedule continuation")
	}
	for range 4 {
		parent.Render(81)
	}
	final := parent.Render(81)
	if len(final) != 130 || final[0] != "0:81" || final[129] != "129:81" {
		t.Fatal("resize did not publish complete, ordered new-width frame")
	}
}

// A resize must re-wrap real Markdown at the new width. The incremental line
// cache once reused block lines rendered at the old width, so a scrolled
// transcript kept its old width and the renderer truncated it.
func TestTranscriptResizeRerendersMarkdownWidth(t *testing.T) {
	mdTheme := GetMarkdownTheme()
	chat := &tui.Container{}
	for range 5 {
		text := "# Heading\n\nA long paragraph that must wrap differently at each width, repeated for the test body. "
		chat.AddChild(tui.NewMarkdown(text+text, 1, 0, mdTheme, nil, tui.MarkdownOptions{}))
	}
	document := newResizeDocument(chat, func() {})
	document.Render(80)
	lines := document.Render(40)
	for _, line := range lines {
		if tui.VisibleWidth(line) > 40 {
			t.Fatalf("published line wider than the frame: %q (%d)", line, tui.VisibleWidth(line))
		}
	}
}
