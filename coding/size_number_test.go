package coding

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/mcp/protocol"
)

// pi v1.5.2 core/tools/truncate.ts formatSize takes a JavaScript number, and a
// resources/list or resource-link entry may carry a fractional or very large
// `size`; an integer-only formatter could not render it.
func TestFormatSizeNumberRendersJavaScriptSizes(t *testing.T) {
	for _, test := range []struct {
		bytes float64
		want  string
	}{
		{1.5, "1.5B"},
		{1536.5, "1.5KB"},
		{1e20, "95367431640625.0MB"},
	} {
		if got := FormatSizeNumber(test.bytes); got != test.want {
			t.Errorf("FormatSizeNumber(%v) = %q, want %q", test.bytes, got, test.want)
		}
	}
	// Control: the integer entry point keeps its existing output.
	if got := FormatSize(2048); got != "2.0KB" {
		t.Fatalf("FormatSize(2048) = %q, want 2.0KB", got)
	}
}

// A resource link whose size is fractional renders it instead of failing the
// result conversion.
func TestConvertMcpResultRendersAFractionalResourceSize(t *testing.T) {
	size := 1.5
	result := &protocol.CallToolResult{Content: []protocol.ContentBlock{{
		Type: protocol.ContentTypeResourceLink, URI: "file:///x", Name: "x", Size: &size,
	}}}
	converted, _, err := ConvertMcpResult("srv", "tool", result, ConvertMcpResultOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(converted.Content) != 1 {
		t.Fatalf("content blocks = %d, want 1", len(converted.Content))
	}
	text, ok := converted.Content[0].(ai.TextContent)
	if !ok || !strings.Contains(text.Text, "(1.5B)") {
		t.Fatalf("resource link = %#v, want a (1.5B) detail", converted.Content[0])
	}
}
