package interactive

import (
	"strings"
	"testing"
)

func TestEditToolHeaderPrefersFilePathOverPath(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)

	component := NewToolExecutionComponent("edit", "call-1", map[string]any{
		"path":      "/tmp/fallback.txt",
		"file_path": "/tmp/preferred.txt",
		"edits": []any{map[string]any{
			"oldText": "old",
			"newText": "new",
		}},
	}, ToolExecutionOptions{}, &editRenderers, nil, "/tmp")

	got := strings.Join(component.Render(80), "\n")
	if !strings.Contains(got, "preferred.txt") {
		t.Fatalf("edit header %q does not show file_path", got)
	}
	if strings.Contains(got, "fallback.txt") {
		t.Fatalf("edit header %q shows path instead of file_path", got)
	}
}
