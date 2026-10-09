package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

// outputPad reaches tool output: the content box follows the setting instead of the hardcoded
// padding it carried before (upstream #10557, which extended outputPad from chat messages to
// every transcript block).
func TestToolOutputFollowsOutputPad(t *testing.T) {
	installPierThemeForTest(t)
	InitTheme("dark", false)
	t.Cleanup(func() { SetRegisteredThemes(nil) })

	definition := builtinToolRenderers["bash"]
	render := func(padding int) []string {
		component := NewToolExecutionComponent("bash", "call-1", map[string]any{"command": "ls"}, ToolExecutionOptions{}, &definition, nil, "/tmp")
		component.SetOutputPad(padding)
		component.MarkExecutionStarted()
		component.SetArgsComplete()
		component.UpdateResult(&SortToolResultContent{
			Content: []ToolResultContent{{Type: "text", Text: "one\ntwo"}},
		}, false)
		return component.Render(40)
	}

	padded := render(1)
	unpadded := render(0)
	if len(padded) != len(unpadded) {
		t.Fatalf("padding changed the line count: %d padded, %d unpadded", len(padded), len(unpadded))
	}
	indented := 0
	for index := range padded {
		withPad := coding.StripAnsi(padded[index])
		withoutPad := coding.StripAnsi(unpadded[index])
		if strings.TrimSpace(withPad) == strings.TrimSpace(withoutPad) {
			// The same text: the padded line must be indented further than the unpadded one.
			if strings.HasPrefix(withPad, " ") != strings.HasPrefix(withoutPad, " ") {
				indented++
			}
			continue
		}
		t.Fatalf("line %d differs beyond its padding: %q vs %q", index, withPad, withoutPad)
	}
	if indented == 0 {
		t.Fatal("tool output ignored outputPad: no line was indented")
	}

	// The content is still there, so the padding did not eat it.
	if !strings.Contains(coding.StripAnsi(strings.Join(unpadded, "\n")), "one") {
		t.Fatalf("unpadded render lost the output: %q", unpadded)
	}
}
