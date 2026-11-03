package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/mcp/protocol"
)

// Reference: pi v1.5.0 f9a26365 extensions/mcp/tools.ts blockToContent
// decodes with Buffer.from(blob, "base64"). A lone final alphabet character
// is ignored whether or not an '=' padding marker follows it.
func TestMcpResourceIgnoresLoneBase64TailBeforePadding(t *testing.T) {
	for _, blob := range []string{"YWJjZ=", "YWJjZ", "YWJj"} {
		content, err := ToModelContent("docs", []protocol.ContentBlock{{
			Type: protocol.ContentTypeResource,
			Resource: &protocol.ResourceContents{
				URI: "docs://tail", MimeType: new("text/plain"), Blob: blob, BlobArm: true,
			},
		}}, ConvertMcpResultOptions{})
		if err != nil || len(content) != 1 {
			t.Fatalf("blob %q: content=%+v, err=%v", blob, content, err)
		}
		if text, ok := content[0].(ai.TextContent); !ok || text.Text != "abc" {
			t.Errorf("blob %q: content=%+v, want abc", blob, content)
		}
	}
}
