package protocol

import (
	"encoding/json"
	"testing"
)

// pi v1.5.2 packages/mcp/src/protocol/types.ts and content.ts type a resource
// or resource link's `size` as a JavaScript number, so a fractional or large
// value is valid. Go's int64 rejected both at decode time.
func TestContentSizesAcceptJavaScriptNumbers(t *testing.T) {
	var resource Resource
	if err := json.Unmarshal([]byte(`{"uri":"file:///x","name":"x","size":1.5}`), &resource); err != nil {
		t.Fatalf("fractional resource size: %v", err)
	}
	if resource.Size == nil || *resource.Size != 1.5 {
		t.Fatalf("resource size = %v, want 1.5", resource.Size)
	}
	var result CallToolResult
	if err := json.Unmarshal([]byte(`{"content":[{"type":"resource_link","uri":"file:///x","name":"x","size":1e20}]}`), &result); err != nil {
		t.Fatalf("large resource link size: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Size == nil || *result.Content[0].Size != 1e20 {
		t.Fatalf("resource link size = %v, want 1e20", result.Content)
	}
	// Control: an integer size still decodes to the same number.
	if err := json.Unmarshal([]byte(`{"uri":"file:///x","name":"x","size":2048}`), &resource); err != nil || *resource.Size != 2048 {
		t.Fatalf("integer size = %v err=%v", resource.Size, err)
	}
}
