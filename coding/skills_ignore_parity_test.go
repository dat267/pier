package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Source-generated pi v1.4.0 (4a080252) core/skills.ts discovery fixtures.
// Regenerate: node coding/testdata/skills-ignore/generate.mjs ./pi > coding/testdata/skills-ignore/golden.json
func TestSkillsIgnoreDiscoveryMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/skills-ignore/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SkillFiles map[string]string `json:"skillFiles"`
		AllNames   []string          `json:"allNames"`
		Cases      []struct {
			Name        string            `json:"name"`
			IgnoreFiles map[string]string `json:"ignoreFiles"`
			Names       []string          `json:"names"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			root := t.TempDir()
			for _, files := range []map[string]string{fixture.SkillFiles, test.IgnoreFiles} {
				for path, content := range files {
					fullPath := filepath.Join(root, filepath.FromSlash(path))
					if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			loaded := LoadSkillsFromDir(root)
			names := []string{}
			for _, skill := range loaded.Skills {
				names = append(names, skill.Name)
			}
			sort.Strings(names)
			wantNames := append([]string(nil), test.Names...)
			sort.Strings(wantNames)
			if !reflect.DeepEqual(names, wantNames) {
				t.Errorf("skill names = %v, want %v", names, wantNames)
			}
			if len(loaded.Diagnostics) != 0 {
				t.Errorf("diagnostics = %+v, want none", loaded.Diagnostics)
			}
			prompt := FormatSkillsForPrompt(loaded.Skills, "read")
			expected := make(map[string]bool, len(test.Names))
			for _, name := range test.Names {
				expected[name] = true
			}
			for _, name := range fixture.AllNames {
				present := strings.Contains(prompt, "<name>"+name+"</name>")
				if present != expected[name] {
					t.Errorf("prompt contains %s = %t, want %t", name, present, expected[name])
				}
			}
		})
	}
}
