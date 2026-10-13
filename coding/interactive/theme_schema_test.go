package interactive

import (
	"encoding/json"
	"testing"
)

// v1.2.0 moved the published theme schema to https://pi.dev/schemas/theme.schema.json.
func TestBuiltinThemesReferencePublishedSchema(t *testing.T) {
	for _, name := range []string{"dark", "light"} {
		data, err := builtinThemeFS.ReadFile("themes/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		if doc["$schema"] != "https://pi.dev/schemas/theme.schema.json" {
			t.Fatalf("%s $schema = %v", name, doc["$schema"])
		}
	}
}
