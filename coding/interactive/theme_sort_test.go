package interactive

import (
	"slices"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) modes/interactive/theme/theme.ts sorts theme
// infos with `a.name.localeCompare(b.name)` (the system theme first). Pier
// compared lowercased bytes, so an accented name sorted after every ASCII name
// instead of with its base letter.
func TestAvailableThemesSortsWithLocaleCollation(t *testing.T) {
	previous := registeredThemesSnapshot()
	SetRegisteredThemes([]*Theme{
		{Name: "zeta"},
		{Name: "ecru"},
		{Name: "Éclair"},
		{Name: "apple"},
	})
	t.Cleanup(func() {
		restore := make([]*Theme, 0, len(previous))
		for _, theme := range previous {
			restore = append(restore, theme)
		}
		SetRegisteredThemes(restore)
	})

	var custom []string
	for _, name := range AvailableThemes() {
		switch name {
		case "zeta", "ecru", "Éclair", "apple":
			custom = append(custom, name)
		}
	}
	want := []string{"apple", "Éclair", "ecru", "zeta"}
	if !slices.Equal(custom, want) {
		t.Fatalf("order = %v, want %v", custom, want)
	}
}
