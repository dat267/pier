package interactive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

// TestTreeRenderDoesNotDecodeRowsAgain pins A496: a row displays a 200-character
// preview, so a render must not decode the whole message again. With 20 visible
// rows of 400 KB content the decode allocated 16.2 MB and took 21 ms per frame,
// once per keystroke and animation tick.
func TestTreeRenderDoesNotDecodeRowsAgain(t *testing.T) {
	InitTheme("dark", false)
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	var body strings.Builder
	body.WriteString(`{"type":"session","version":3,"id":"big","timestamp":"2026-01-01T00:00:00.000Z","cwd":"/tmp"}` + "\n")
	content := strings.Repeat("x", 400_000)
	for turn := 0; turn < 20; turn++ {
		message, err := json.Marshal(map[string]any{"role": "user", "content": content})
		if err != nil {
			t.Fatal(err)
		}
		entry, err := json.Marshal(map[string]any{
			"type": "message", "id": fmt.Sprintf("m%d", turn),
			"timestamp": "2026-01-01T00:00:00.000Z", "message": json.RawMessage(message),
		})
		if err != nil {
			t.Fatal(err)
		}
		body.Write(entry)
		body.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	session, err := coding.OpenSession(path, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	selector := NewTreeSelectorComponent(session.GetTree(), session.GetLeafID(), 40, func(string) {}, func() {}, TreeSelectorOptions{})
	_ = selector.Render(100) // warm the previews

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_ = selector.Render(100)
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 2<<20 {
		t.Fatalf("one render allocated %d bytes; the row previews are being decoded again", allocated)
	}
}
