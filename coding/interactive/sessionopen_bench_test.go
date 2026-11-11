package interactive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

// writeBenchSession writes a session file shaped like a long real one: an
// assistant turn with tool arguments, then a tool result carrying a few KB of
// output, repeated.
func writeBenchSession(b *testing.B, turns int) string {
	b.Helper()
	dir := b.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	var builder strings.Builder
	builder.WriteString(`{"type":"session","version":3,"id":"bench","timestamp":"2026-01-01T00:00:00.000Z","cwd":"/tmp"}` + "\n")
	parent := ""
	body := strings.Repeat("output line\n", 250)
	for turn := range turns {
		args, err := json.Marshal(map[string]any{"command": "cargo test --workspace", "path": "coding/session_manager.go", "note": body})
		if err != nil {
			b.Fatal(err)
		}
		assistant, err := json.Marshal(map[string]any{
			"role": "assistant", "stopReason": "toolUse",
			"content": []any{map[string]any{"type": "toolCall", "id": fmt.Sprintf("t%d", turn), "name": "bash", "arguments": json.RawMessage(args)}},
		})
		if err != nil {
			b.Fatal(err)
		}
		result, err := json.Marshal(map[string]any{
			"role": "toolResult", "toolCallId": fmt.Sprintf("t%d", turn), "toolName": "bash",
			"content": []any{map[string]any{"type": "text", "text": body}},
		})
		if err != nil {
			b.Fatal(err)
		}
		for index, message := range []json.RawMessage{assistant, result} {
			id := fmt.Sprintf("m%05d", turn*2+index)
			parentField := ""
			if parent != "" {
				parentField = fmt.Sprintf(`,"parentId":%q`, parent)
			}
			fmt.Fprintf(&builder, `{"type":"message","id":%q,"timestamp":"2026-01-01T00:00:00.000Z"%s,"message":%s}`+"\n",
				id, parentField, message)
			parent = id
		}
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	return path
}

func openBenchSession(b *testing.B) (*coding.SessionManager, string) {
	b.Helper()
	InitTheme("dark", false)
	path := writeBenchSession(b, 500)
	m, err := coding.OpenSession(path, filepath.Dir(path), "")
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	return m, path
}

// BenchmarkTreeOpen guards /tree's open cost: the list decodes every entry once,
// and the open runs off the owner loop.
func BenchmarkTreeOpen(b *testing.B) {
	m, _ := openBenchSession(b)
	tree := m.GetTree()
	leaf := m.GetLeafID()
	b.ReportAllocs()

	b.Run("GetTree", func(b *testing.B) {
		for range b.N {
			if len(m.GetTree()) != len(tree) {
				b.Fatal("tree changed between runs")
			}
		}
	})
	b.Run("NewTreeSelector", func(b *testing.B) {
		for range b.N {
			NewTreeSelectorComponent(tree, leaf, 40, func(string) {}, func() {}, TreeSelectorOptions{})
		}
	})
	b.Run("FirstFrame", func(b *testing.B) {
		for range b.N {
			selector := NewTreeSelectorComponent(tree, leaf, 40, func(string) {}, func() {}, TreeSelectorOptions{})
			if lines := selector.Render(80); len(lines) == 0 {
				b.Fatal("no lines")
			}
		}
	})
}

// BenchmarkTreeRender guards the per-frame cost while the selector is open: the
// visible rows decode only their own tool-call arguments.
func BenchmarkTreeRender(b *testing.B) {
	m, _ := openBenchSession(b)
	selector := NewTreeSelectorComponent(m.GetTree(), m.GetLeafID(), 40, func(string) {}, func() {}, TreeSelectorOptions{})
	_ = selector.Render(80)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if lines := selector.Render(80); len(lines) == 0 {
			b.Fatal("no lines")
		}
	}
}

// BenchmarkSessionExport guards /export's cost: serializing the branch re-chains
// and re-marshals each entry, and the command runs it off the owner loop.
func BenchmarkSessionExport(b *testing.B) {
	m, path := openBenchSession(b)
	output := filepath.Join(filepath.Dir(path), "out.jsonl")
	b.ReportAllocs()
	for range b.N {
		if _, err := coding.ExportSessionToJsonl(m, output, nil); err != nil {
			b.Fatal(err)
		}
	}
}
