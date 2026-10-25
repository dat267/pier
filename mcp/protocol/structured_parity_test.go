package protocol

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 protocol/content.ts calls JSON.stringify on the decoded structured
// value. Regenerate: node mcp/protocol/testdata/structured/generate.mjs ./pi > mcp/protocol/testdata/structured/fixtures.json
func TestStructuredToolContentAgainstUpstream(t *testing.T) {
	data, err := os.ReadFile("testdata/structured/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct{ Input, Text string }
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		content := Content(&CallToolResult{StructuredContent: json.RawMessage(fixture.Input)})
		if len(content) != 1 {
			t.Fatalf("input %s: got %d blocks, want one", fixture.Input, len(content))
		}
		text, ok := content[0].(ai.TextContent)
		if !ok || text.Text != fixture.Text {
			t.Errorf("input %s: got %#v, upstream text = %q", fixture.Input, content[0], fixture.Text)
		}
	}
}
