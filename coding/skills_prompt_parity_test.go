package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), core/skills.ts formatSkillsForPrompt.
// Regenerate the golden with
// `node --experimental-strip-types coding/testdata/skills-prompt/generate.mjs ./pi`.
// The fixture also carries a skill with disable-model-invocation, which is
// listed by discovery but hidden from the prompt.
func TestFormattedSkillsMatchUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/skills-prompt/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Skills []struct {
			Name                   string `json:"name"`
			Description            string `json:"description"`
			DisableModelInvocation bool   `json:"disableModelInvocation"`
		} `json:"skills"`
		Prompts map[string]string `json:"prompts"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.Abs(filepath.Join("testdata", "skills-prompt"))
	if err != nil {
		t.Fatal(err)
	}
	result := LoadSkillsFromDir(dir)
	if len(result.Skills) != len(golden.Skills) {
		t.Fatalf("skills = %d, want %d", len(result.Skills), len(golden.Skills))
	}
	for index, skill := range result.Skills {
		want := golden.Skills[index]
		if skill.Name != want.Name || skill.Description != want.Description || skill.DisableModelInvocation != want.DisableModelInvocation {
			t.Errorf("skill %d = %q/%q/%v, want %q/%q/%v", index,
				skill.Name, skill.Description, skill.DisableModelInvocation,
				want.Name, want.Description, want.DisableModelInvocation)
		}
	}
	for _, tool := range []string{"read", "bash", "indirect"} {
		got := FormatSkillsForPrompt(result.Skills, tool)
		for _, absolute := range []string{dir, filepath.ToSlash(dir)} {
			got = strings.ReplaceAll(got, absolute, "<dir>")
		}
		if got != golden.Prompts[tool] {
			t.Errorf("prompt for %s differs:\n got %q\nwant %q", tool, got, golden.Prompts[tool])
		}
	}
}
