package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) packages/ai/src/utils/validation.ts caches a
// compiled validator in a WeakMap keyed by the schema object, so a boolean
// schema (`true` or `false`) throws "Invalid value used as weak map key" before
// validation runs. D477: pier compiles the boolean schema instead, so `true`
// accepts every value and `false` rejects every value with the usual message.
func TestValidateToolArgumentsAcceptsBooleanRootSchemas(t *testing.T) {
	accepting := Tool{Name: "t", Parameters: json.RawMessage(`true`)}
	for _, args := range []string{`1`, `null`, `{}`, `[1]`} {
		got, err := ValidateToolArguments(accepting, ToolCall{Name: "t", Arguments: json.RawMessage(args)})
		if err != nil {
			t.Fatalf("true schema rejected %s: %v", args, err)
		}
		if string(got) != args {
			t.Fatalf("true schema returned %s for %s", got, args)
		}
	}
	rejecting := Tool{Name: "t", Parameters: json.RawMessage(`false`)}
	_, err := ValidateToolArguments(rejecting, ToolCall{Name: "t", Arguments: json.RawMessage(`1`)})
	if err == nil {
		t.Fatal("false schema accepted a value")
	}
	if !strings.Contains(err.Error(), "root: no value is allowed here") {
		t.Fatalf("err = %v, want the root rejection message", err)
	}
}
