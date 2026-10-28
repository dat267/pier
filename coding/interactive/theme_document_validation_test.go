package interactive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), theme-schema.ts ColorValueSchema accepts only a
// string or an integer; null must not become Go's zero (palette index black).
func TestThemeValidationRejectsNullColorValues(t *testing.T) {
	valid, err := os.ReadFile("themes/dark.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"colors", "vars"} {
		var document map[string]any
		if err := json.Unmarshal(valid, &document); err != nil {
			t.Fatal(err)
		}
		key := "accent"
		if scope == "vars" {
			key = "invalid"
		}
		document[scope].(map[string]any)[key] = nil
		encoded, err := ai.MarshalJSON(document)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ValidateThemeJSON("invalid", encoded); err == nil {
			t.Errorf("null %s/%s was accepted as black", scope, key)
		}
	}
}

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
