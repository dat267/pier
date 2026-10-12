//go:build linux

package interactive

import "testing"

// theme.ts receives UTF-8-decoded names from readdir before trying to load each theme.
func TestCustomThemesSkipInvalidUTF8FileNames(t *testing.T) {
	dir := t.TempDir()
	SetCustomThemesDir(dir)
	t.Cleanup(func() { SetCustomThemesDir("") })
	writeThemeFile(t, dir, string([]byte{0xff})+".json", "bad", "#ff0000")
	if got := customThemeInfos(); len(got) != 0 {
		t.Fatalf("themes with invalid UTF-8 path = %+v; want skipped", got)
	}
}
