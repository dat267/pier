package interactive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
)

// TestEditToolRenderUpstreamParity pins the edit tool block's rendered bytes
// against pi v1.4.0 (4a080252), ToolExecutionComponent and tools/renderers/edit.ts,
// using pier's palette (D154). Regenerate from the repository root:
//
//	node --experimental-strip-types coding/interactive/testdata/gen-edittool-parity.mjs ./pi > coding/interactive/testdata/edittool_parity_golden.txt
//
// It covers the self-shell layout, one horizontal/vertical padding cell, and
// diff line formatting ("-1 ", "+1 ", " 2 " context normalization).
func TestEditToolRenderUpstreamParity(t *testing.T) {
	// The golden was recorded on a Unix host driving /tmp/pier_parity and embeds
	// that path. The renderer prints the path it was given (filepath.Join's form,
	// i.e. the platform separator), so the comparison only holds on Unix.
	if runtime.GOOS == "windows" {
		t.Skip("the golden embeds the Unix host path /tmp/pier_parity")
	}
	dir := "/tmp/pier_parity"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Skipf("cannot create %s: %v", dir, err)
	}
	file := filepath.Join(dir, "f.txt")
	content := "line 1: keep\nline 2: old value\nline 3: keep\nline 4: old value again\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Skipf("cannot write %s: %v", file, err)
	}

	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)

	oldContent := "line 1: keep\nline 2: old value\nline 3: keep\nline 4: old value again\n"
	newContent := "line 1: multi\nline 2: old value\nline 3: multi too\nline 4: old value again\n"
	diffString, _, _ := coding.GenerateDiffString(oldContent, newContent, 3)

	component := NewToolExecutionComponent("edit", "call-1",
		map[string]any{
			"path": file,
			"edits": []any{
				map[string]any{"oldText": "line 1: keep", "newText": "line 1: multi"},
				map[string]any{"oldText": "line 3: keep", "newText": "line 3: multi too"},
			},
		},
		ToolExecutionOptions{}, &editRenderers, nil, dir)

	// The preview starts only once streamed arguments are complete.
	component.SetArgsComplete()
	// Wait for the async preview worker (computeEditsPreview) to publish.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if state, ok := component.rendererState.(*editCallComponent); ok && state.snapshotPreview() != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	state, ok := component.rendererState.(*editCallComponent)
	if !ok || state.snapshotPreview() == nil {
		t.Fatal("edit preview did not publish")
	}

	first := 1
	component.UpdateResult(&SortToolResultContent{
		Content: []ToolResultContent{{Type: "text", Text: "Edited " + file}},
		Details: &coding.EditToolDetails{Diff: diffString, Patch: "", FirstChangedLine: &first},
	}, false)

	var got []string
	for _, line := range component.Render(80) {
		b, err := json.Marshal(line)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		got = append(got, string(b))
	}

	want, err := os.ReadFile(filepath.Join("testdata", "edittool_parity_golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	expected := splitGoldenLines(string(want))
	if len(got) != len(expected) {
		t.Fatalf("line count = %d, want %d\ngot:\n%s", len(got), len(expected), joinQuoted(got))
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Fatalf("line %d:\n got %s\nwant %s", i, got[i], expected[i])
		}
	}
}

func splitGoldenLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func joinQuoted(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

// Reference: pi v1.5.2 renderers/edit.ts buildEditCallComponent applies the
// transcript padding to its self shell with setPaddingX(context.outputPad), and
// renderResult pads the result text with the same value.
func TestEditToolAppliesOutputPad(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	component := NewToolExecutionComponent("edit", "call-1", map[string]any{
		"path":  file,
		"edits": []any{map[string]any{"oldText": "old", "newText": "new"}},
	}, ToolExecutionOptions{}, &editRenderers, nil, dir)
	component.SetOutputPad(3)
	component.SetArgsComplete()
	component.UpdateResult(&SortToolResultContent{
		Content: []ToolResultContent{{Type: "text", Text: "Edit failed"}},
		IsError: true,
	}, false)

	lines := component.Render(80)
	var call, result string
	for _, line := range lines {
		visible := coding.StripAnsi(line)
		if strings.Contains(visible, "edit ") && call == "" {
			call = visible
		}
		if strings.Contains(visible, "Edit failed") && result == "" {
			result = visible
		}
	}
	if call == "" || !strings.HasPrefix(call, "   edit ") {
		t.Fatalf("call line = %q, want three spaces of padding", call)
	}
	if result == "" || !strings.HasPrefix(result, "   Edit failed") {
		t.Fatalf("result line = %q, want three spaces of padding", result)
	}
}

// Reference: pi v1.5.2 renderers/edit.ts getRenderablePreviewInput falls back to
// the legacy oldText/newText pair when the edits array is unusable.
func TestEditPreviewFallsBackToLegacyFields(t *testing.T) {
	args := map[string]any{
		"path":    "/tmp/a.txt",
		"edits":   []any{map[string]any{"oldText": "only-old"}},
		"oldText": "a",
		"newText": "b",
	}
	preview := getRenderablePreviewInput(args)
	if preview == nil || len(preview.Edits) != 1 || preview.Edits[0].OldText != "a" || preview.Edits[0].NewText != "b" {
		t.Fatalf("preview = %+v, want the legacy pair", preview)
	}
	// A usable edits array still wins over the legacy fields.
	both := map[string]any{
		"path":    "/tmp/a.txt",
		"edits":   []any{map[string]any{"oldText": "c", "newText": "d"}},
		"oldText": "a",
		"newText": "b",
	}
	if preview := getRenderablePreviewInput(both); preview == nil || preview.Edits[0].OldText != "c" {
		t.Fatalf("preview = %+v, want the edits array", preview)
	}
}

// Reference: pi v1.5.2 renderers/edit.ts renderResult publishes details.diff
// whenever it is a string, so an empty diff clears a stale preview error.
func TestEditResultEmptyDiffClearsPreviewError(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)
	file := "/tmp/pier_empty_diff/f.txt"
	component := NewToolExecutionComponent("edit", "call-1", map[string]any{
		"path":  file,
		"edits": []any{map[string]any{"oldText": "old", "newText": "new"}},
	}, ToolExecutionOptions{}, &editRenderers, nil, "/tmp/pier_empty_diff")
	state, ok := component.rendererState.(*editCallComponent)
	if !ok {
		t.Fatalf("state = %T", component.rendererState)
	}
	key := argsKeyFor(&editRenderArgs{
		Path: file, HasPath: true,
		Edits: []coding.Edit{{OldText: "old", NewText: "new"}},
	})
	state.preview.Store(&editPreviewState{argsKey: key, preview: &editPreview{Error: "preview failed"}})
	component.UpdateResult(&SortToolResultContent{
		Content: []ToolResultContent{{Type: "text", Text: "Edited " + file}},
		Details: &coding.EditToolDetails{Diff: ""},
	}, false)
	if rendered := coding.StripAnsi(strings.Join(component.Render(80), "\n")); strings.Contains(rendered, "preview failed") {
		t.Fatalf("rendered kept the stale preview error:\n%s", rendered)
	}
}
