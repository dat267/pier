package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) packages/schema/src/coerce.ts pass 3
// (coercePrimitive). It runs only for a type the value does not already match,
// and unlike pass 2 (convertNode) it never truncates an existing number for
// "integer", never reads "true"/"false" as a number, and turns null into ""
// for "string" (pass 2 turns it into "null"). The two passes are separate in
// the reference, so a union type array takes pass 3 alone.
func TestValidateToolArgumentsCoercesUnionsLikePiSchemaPass3(t *testing.T) {
	cases := []struct {
		name     string
		schema   string
		args     string
		want     string // "" means the call must fail validation
		wantFail bool
	}{
		{"integer union keeps the number", `{"type":"object","properties":{"v":{"type":["integer","null"]}}}`,
			`{"v":3.7}`, "", true},
		{"integer union keeps the numeric string", `{"type":"object","properties":{"v":{"type":["integer","null"]}}}`,
			`{"v":"3.7"}`, "", true},
		{"integer union converts a non-number to the string branch", `{"type":"object","properties":{"v":{"type":["integer","string"]}}}`,
			`{"v":3.7}`, `{"v":"3.7"}`, false},
		{"number union reads a word as the boolean branch", `{"type":"object","properties":{"v":{"type":["number","boolean"]}}}`,
			`{"v":"true"}`, `{"v":true}`, false},
		{"number union reads false as the boolean branch", `{"type":"object","properties":{"v":{"type":["number","boolean"]}}}`,
			`{"v":"false"}`, `{"v":false}`, false},
		{"number union does not read a word as a number", `{"type":"object","properties":{"v":{"type":["number","null"]}}}`,
			`{"v":"true"}`, "", true},
		{"string union turns a required null into the empty string", `{"type":["string","number"]}`,
			`null`, `""`, false},
		{"integer union reads true as the boolean branch", `{"type":"object","properties":{"v":{"type":["integer","boolean"]}}}`,
			`{"v":"true"}`, `{"v":true}`, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tool := Tool{Name: "t", Parameters: json.RawMessage(testCase.schema)}
			got, err := ValidateToolArguments(tool, ToolCall{Name: "t", Arguments: json.RawMessage(testCase.args)})
			if testCase.wantFail {
				if err == nil {
					t.Fatalf("%s = %s, want a validation error", testCase.args, got)
				}
				if !strings.Contains(err.Error(), "Validation failed") {
					t.Fatalf("err = %v, want a validation failure", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", testCase.args, err)
			}
			if string(got) != testCase.want {
				t.Fatalf("%s = %s, want %s", testCase.args, got, testCase.want)
			}
		})
	}
}
