package interactive

import (
	"encoding/json"
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) theme-schema.ts validateThemeJson checks the
// name before the schema, so a document with a "/" in its name reports the name
// even when colors are missing. Its schema sets additionalProperties: false, so
// an unknown top-level field (or export field) is an error; pier ignored them
// and reported the missing colors instead.
func TestThemeValidationMatchesTheReferenceOrderAndExtraKeys(t *testing.T) {
	// A name with a slash wins over missing colors.
	if _, err := ValidateThemeJSON("slash", json.RawMessage(`{"name":"a/b","colors":{}}`)); err == nil ||
		!strings.Contains(err.Error(), `Invalid theme name "a/b"`) {
		t.Fatalf("slash-name err = %v", err)
	}

	valid := func(extra string) json.RawMessage {
		colors := map[string]any{}
		for _, name := range requiredThemeColors {
			colors[name] = "#333333"
		}
		document := `{"name":"custom","colors":` + mustJSON(colors) + extra + `}`
		return json.RawMessage(document)
	}
	if _, err := ValidateThemeJSON("valid", valid("")); err != nil {
		t.Fatalf("valid theme err = %v", err)
	}
	_, err := ValidateThemeJSON("extra", valid(`,"color":"#000000"`))
	if err == nil || !strings.Contains(err.Error(), "  - /color: additional properties are not allowed") {
		t.Fatalf("extra top-level key err = %v", err)
	}
	_, err = ValidateThemeJSON("export", json.RawMessage(strings.TrimSuffix(string(valid("")), "}")+
		`,"export":{"pageBg":"#000000","page":"#ffffff"}}`))
	if err == nil || !strings.Contains(err.Error(), "  - /export/page: additional properties are not allowed") {
		t.Fatalf("extra export key err = %v", err)
	}
}
