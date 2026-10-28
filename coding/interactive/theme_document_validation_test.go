package interactive

import (
	"os"
	"testing"
)

// pi v1.4.0 (4a080252), theme.ts parseThemeJsonContent parses the entire document
// with JSON.parse before calling theme-schema.ts validateThemeJson.
func TestThemeValidationRejectsTrailingJSONData(t *testing.T) {
	valid, err := os.ReadFile("themes/dark.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateThemeJSON("control", valid); err != nil {
		t.Fatalf("valid theme control failed: %v", err)
	}
	for _, suffix := range []string{" trailing", " {}"} {
		if _, err := ValidateThemeJSON("invalid", append(append([]byte(nil), valid...), suffix...)); err == nil {
			t.Errorf("theme accepted trailing data %q", suffix)
		}
	}
}
