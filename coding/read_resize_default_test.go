package coding

import (
	"bytes"
	"encoding/base64"
	"github.com/dat267/pier/ai"
	"image"
	"os"
	"path/filepath"
	"testing"
)

// Reference: pi v1.3.0 core/tools/read.ts defaults omitted autoResizeImages to true.
func TestReadModelOptionKeepsDefaultResize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wide.png"), solidPNG(t, 2001, 1), 0600); err != nil {
		t.Fatal(err)
	}
	result := execTool(t, CreateReadTool(dir, &ReadToolOptions{Model: func() *ai.Model { return nil }}), `{"path":"wide.png"}`)
	data, err := base64.StdEncoding.DecodeString(result.Content[1].(ai.ImageContent).Data)
	if err != nil {
		t.Fatal(err)
	}
	size, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if size.Width != 2000 {
		t.Fatalf("width=%d; want 2000", size.Width)
	}
}
