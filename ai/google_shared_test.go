package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// pi v1.5.0 google-shared.ts rewriteConstAsEnum: Gemini ignores `const` in
// parametersJsonSchema, so string and number constants become single-value
// enums; a property named "const" and a schema that already has an enum stay.
func TestRewriteGoogleConstAsEnum(t *testing.T) {
	cases := map[string]struct {
		schema string
		want   string
	}{
		"string const": {
			`{"type":"object","properties":{"mode":{"type":"string","const":"fast"}},"required":["mode"]}`,
			`{"type":"object","properties":{"mode":{"type":"string","enum":["fast"]}},"required":["mode"]}`,
		},
		"number const": {
			`{"type":"object","properties":{"n":{"const":3,"type":"number"}}}`,
			`{"type":"object","properties":{"n":{"type":"number","enum":[3]}}}`,
		},
		"property named const": {
			`{"type":"object","properties":{"const":{"type":"string","const":"x"}}}`,
			`{"type":"object","properties":{"const":{"type":"string","enum":["x"]}}}`,
		},
		"enum already present": {
			`{"type":"object","properties":{"m":{"const":"a","enum":["a","b"]}}}`,
			`{"type":"object","properties":{"m":{"const":"a","enum":["a","b"]}}}`,
		},
		"boolean const unchanged": {
			`{"type":"object","properties":{"b":{"const":true}}}`,
			`{"type":"object","properties":{"b":{"const":true}}}`,
		},
		"array of subschemas": {
			`{"allOf":[{"const":"x"}]}`,
			`{"allOf":[{"enum":["x"]}]}`,
		},
	}
	for name, probe := range cases {
		if got := string(rewriteGoogleConstAsEnum(json.RawMessage(probe.schema), false)); got != probe.want {
			t.Fatalf("%s = %s, want %s", name, got, probe.want)
		}
	}
}

// The Gemini function declaration sends the rewritten schema.
func TestConvertGoogleToolsRewritesConstToEnum(t *testing.T) {
	tools := []Tool{{
		Name: "probe", Description: "d",
		Parameters: json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string","const":"fast"}}}`),
	}}
	groups, err := ConvertGoogleTools(tools, false, false)
	if err != nil {
		t.Fatal(err)
	}
	schema := string(groups[0].FunctionDeclarations[0].ParametersJSONSchema)
	if !strings.Contains(schema, `"enum":["fast"]`) || strings.Contains(schema, `"const"`) {
		t.Fatalf("parametersJsonSchema = %s", schema)
	}
}
