package coding

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// pi v1.4.0 core/skills.ts loadSkillFromFile validates both description and
// name before rejecting a declared skill whose description is empty.
func TestMissingSkillDescriptionStillReportsInvalidName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: Bad--Name\ndescription: \"\"\n---\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := LoadSkillsFromDir(dir)
	var messages []string
	for _, diagnostic := range result.Diagnostics {
		messages = append(messages, diagnostic.Message)
	}
	want := []string{"description is required", "name contains invalid characters (must be lowercase a-z, 0-9, hyphens only)", "name must not contain consecutive hyphens"}
	if len(result.Skills) != 0 || !reflect.DeepEqual(messages, want) {
		t.Fatalf("skills=%+v diagnostics=%q, want no skill and diagnostics=%q", result.Skills, messages, want)
	}
}
