package coding

import (
	"os"
	"strings"
	"testing"
)

func TestSkillsOrderStability(t *testing.T) {
	dir := os.Getenv("SKILLS_FIXTURE_DIR")
	if dir == "" {
		t.Skip("SKILLS_FIXTURE_DIR unset")
	}
	for run := 0; run < 3; run++ {
		result := LoadSkillsFromDir(dir)
		var names []string
		for _, skill := range result.Skills {
			names = append(names, skill.Name)
		}
		t.Logf("run %d: %s", run, strings.Join(names, ", "))
	}
}
