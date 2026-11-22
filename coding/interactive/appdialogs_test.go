package interactive

import (
	"strings"
	"testing"
)

// pi v1.5.2 (b4afdddf) modes/interactive/interactive-mode.ts showExtensionConfirm
// renders the selector with `${title}\n${message}` as its title, so the message
// line carries the title's bold accent styling (components/extension-selector.ts
// puts the whole string in `theme.fg("accent", theme.bold(title))`). The port
// passed the message as the selector's description, which renders in the plain
// text colour: the same words, a different line.
func TestAskConfirmRendersTheMessageAsPartOfTheTitle(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	app.askConfirm("Allow?", "Run rm -rf", nil)
	if app.slot.EditorContainer == nil || len(app.slot.EditorContainer.Children) == 0 {
		t.Fatal("askConfirm did not install a selector")
	}
	rendered := strings.Join(app.slot.EditorContainer.Children[0].Render(60), "\n")
	lines := strings.Split(rendered, "\n")
	var titleLine, messageLine string
	for _, line := range lines {
		if strings.Contains(line, "Allow?") {
			titleLine = line
		}
		if strings.Contains(line, "Run rm -rf") {
			messageLine = line
		}
	}
	if titleLine == "" || messageLine == "" {
		t.Fatalf("dialog did not render both lines:\n%s", rendered)
	}
	// The message must carry the title's accent colour, as one bold title string
	// does. Compare the colour prefix of each line.
	if colourOf(titleLine) != colourOf(messageLine) {
		t.Fatalf("message line colour %q differs from title %q in:\n%s", colourOf(messageLine), colourOf(titleLine), rendered)
	}
}

// colourOf returns the first SGR sequence on the line, which is the colour the
// selector applies to the text it starts with.
func colourOf(line string) string {
	if index := strings.Index(line, "\x1b["); index >= 0 {
		if end := strings.IndexByte(line[index:], 'm'); end >= 0 {
			return line[index : index+end+1]
		}
	}
	return ""
}

// The reference always joins `${title}\n${message}`, so an empty message still
// adds the empty second title line.
func TestAskConfirmWithEmptyMessageKeepsTheJoinedTitle(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	var component *ExtensionSelectorComponent
	app.slot.Show(func(done func()) CreatedSelector {
		component = NewExtensionSelectorComponent("Allow?\n", []string{"Yes", "No"}, nil, nil,
			ExtensionSelectorOptions{})
		return CreatedSelector{Component: component}
	})
	if component == nil {
		t.Fatal("no component")
	}
	lines := component.Render(50)
	if len(lines) < 3 {
		t.Fatalf("dialog rendered %d lines, want the empty title line:\n%v", len(lines), lines)
	}
	app.slot.DisposeActiveSelector()
}
