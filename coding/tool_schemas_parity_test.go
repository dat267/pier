package coding

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/tools/{read,write,edit,bash,grep,find,ls}.ts:
// their TypeBox schemas keep construction order at every object level. These
// bytes are sent in provider declarations and persisted in system tool deltas.
// Regenerate: node testdata/tool-schemas/generate.mjs ../pi > testdata/tool-schemas/golden.json
func TestBuiltinToolSchemasMatchUpstream(t *testing.T) {
	data, err := os.ReadFile("testdata/tool-schemas/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct{ Name, Parameters string }
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			tool, err := CreateTool(fixture.Name, t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := ai.MarshalJSON(tool.Parameters)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != fixture.Parameters {
				t.Fatalf("parameters = %s\nwant         %s", encoded, fixture.Parameters)
			}
		})
	}
}
