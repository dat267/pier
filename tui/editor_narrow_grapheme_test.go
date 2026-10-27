package tui

import (
	"context"
	"os"
	"os/exec"
	"runtime/debug"
	"testing"
	"time"
)

// D281: pi v1.4.0 (4a080252), components/editor.ts wordWrapLine recurses on
// one over-wide grapheme without reducing it. A two-column editor leaves one
// layout column, so a CJK glyph crashes rendering. Re-exec contains Go's fatal
// stack overflow; the small child-only stack limit makes the regression fast.
func TestEditorRendersWideGraphemeInNarrowTerminal(t *testing.T) {
	const probeEnv = "PIER_TEST_EDITOR_NARROW_GRAPHEME"
	if mode := os.Getenv(probeEnv); mode != "" {
		debug.SetMaxStack(128 * 1024)
		editor := NewEditor(nil, EditorTheme{}, EditorOptions{})
		editor.SetText("界")
		width := 20
		if mode == "narrow" {
			width = 2
		}
		if lines := editor.Render(width); len(lines) < 3 {
			t.Fatalf("editor render returned no content: %q", lines)
		}
		if editor.GetText() != "界" {
			t.Fatal("narrow rendering changed the editor text")
		}
		return
	}
	for _, mode := range []string{"control", "narrow"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestEditorRendersWideGraphemeInNarrowTerminal$", "-test.timeout=4s")
			command.Env = append(os.Environ(), probeEnv+"="+mode)
			if output, err := command.CombinedOutput(); err != nil {
				if len(output) > 2000 {
					output = output[:2000]
				}
				t.Fatalf("editor render did not complete: %v\n%s", err, output)
			}
		})
	}
}
