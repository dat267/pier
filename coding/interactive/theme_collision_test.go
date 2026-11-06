package interactive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two custom theme files can declare the same name. The reference lists
// readdirSync entries (libuv sorts file names) and keeps the first one, so the
// alphabetically first file wins; the native directory order decided it here.
func TestCustomThemeCollisionResolvesByFileName(t *testing.T) {
	dir := t.TempDir()
	SetCustomThemesDir(dir)
	t.Cleanup(func() { SetCustomThemesDir("") })
	writeTheme := func(fileName string) {
		colors := map[string]any{}
		for _, key := range requiredThemeColors {
			colors[key] = "#000000"
		}
		data, err := json.Marshal(map[string]any{"name": "duplicate", "colors": colors})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fileName), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The loser is written first so native directory order picks it.
	writeTheme("b-theme.json")
	writeTheme("a-theme.json")

	var winner string
	for _, info := range customThemeInfos() {
		if info.Name == "duplicate" {
			winner = info.Path
		}
	}
	if !strings.HasSuffix(winner, "a-theme.json") {
		t.Fatalf("duplicate theme winner = %q, want a-theme.json", winner)
	}
}
