package coding

import (
	"github.com/dat267/pier/ai"
	"os"
	"path/filepath"
	"testing"
)

// Reference: pi v1.3.0 utils/mime.ts accepts a zero-byte sniff;
// core/tools/read.ts returns an empty text block for an empty file.
func TestReadEmptyTextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	result := execTool(t, CreateReadTool(dir, nil), `{"path":"empty.txt"}`)
	if len(result.Content) != 1 || result.Content[0].(ai.TextContent).Text != "" {
		t.Fatalf("result = %+v", result)
	}
}
