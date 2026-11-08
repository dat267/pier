package docscheck

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// AGENTS.md (Structure) fixes the layering: ai and agent must not import coding
// or tui, and tui must not import coding. Two of those violations would not be
// cycles, so the compiler cannot catch them: ai or agent importing tui builds
// fine and silently makes the core depend on terminal rendering. This test
// keeps the documented structure true instead.
func TestPackageImportsRespectTheLayers(t *testing.T) {
	forbidden := map[string][]string{
		"ai":    {"coding", "tui"},
		"agent": {"coding", "tui"},
		"tui":   {"coding"},
	}
	const module = "github.com/dat267/pier/"
	for packageDir, banned := range forbidden {
		files := 0
		err := filepath.WalkDir(filepath.Join(root, packageDir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			files++
			for _, imported := range parsed.Imports {
				importPath := strings.Trim(imported.Path.Value, `"`)
				trimmed, ok := strings.CutPrefix(importPath, module)
				if !ok {
					continue
				}
				for _, blocked := range banned {
					if trimmed == blocked || strings.HasPrefix(trimmed, blocked+"/") {
						t.Errorf("%s imports %s, which AGENTS.md forbids", path, importPath)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", packageDir, err)
		}
		if files == 0 {
			t.Fatalf("no Go files found under %s; the layering check would be empty", packageDir)
		}
	}
}
