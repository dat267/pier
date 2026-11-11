package coding

import (
	"encoding/json"
	"testing"
)

// pi v1.5.2 (b4afdddf) packages/mcp/src/input-schema.ts normalizeMcpInputSchema:
// a server's tool schema is completed to an object schema, so a provider that
// validates the shape still accepts a zero-argument tool. Pier marshals the
// completed schema through a Go map, so its keys come out sorted instead of
// server order followed by type and properties (D458).
func TestMcpToolParametersCompleteTheSchema(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "missing-type", input: `{"properties":{"q":{"type":"string"}}}`,
			want: `{"properties":{"q":{"type":"string"}},"type":"object"}`},
		{name: "missing-properties", input: `{"type":"object"}`,
			want: `{"properties":{},"type":"object"}`},
		{name: "complete-schema-unchanged", input: `{"type":"object","properties":{},"required":["q"]}`,
			want: `{"properties":{},"required":["q"],"type":"object"}`},
		{name: "empty-object", input: `{}`, want: `{"properties":{},"type":"object"}`},
		{name: "empty-input", input: ``, want: `{"properties":{},"type":"object"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(mcpToolParameters(json.RawMessage(tc.input)))
			if got != tc.want {
				t.Fatalf("parameters = %s, want %s", got, tc.want)
			}
		})
	}
}
