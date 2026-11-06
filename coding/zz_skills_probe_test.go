package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Throwaway probe: compare pier's skill discovery with the pinned reference on a
// fixture built by the reference itself.
func TestSkillsDifferential(t *testing.T) {
	path := os.Getenv("SKILLS_PROBE_JSON")
	if path == "" {
		t.Skip("SKILLS_PROBE_JSON unset")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Dir    string `json:"dir"`
		Report struct {
			Skills []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				FilePath    string `json:"filePath"`
			} `json:"skills"`
			Diagnostics []struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"diagnostics"`
		} `json:"report"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	result := LoadSkillsFromDir(fixture.Dir)
	type skillView struct{ name, description, filePath string }
	var got []skillView
	for _, skill := range result.Skills {
		got = append(got, skillView{skill.Name, skill.Description, filepath.ToSlash(skill.FilePath)})
	}
	for i := range got {
		got[i].filePath = replacePrefix(got[i].filePath, filepath.ToSlash(fixture.Dir), "<dir>")
	}
	if len(got) != len(fixture.Report.Skills) {
		t.Fatalf("skills = %d, reference %d\n got %+v\nwant %+v", len(got), len(fixture.Report.Skills), got, fixture.Report.Skills)
	}
	for i := range got {
		want := fixture.Report.Skills[i]
		if got[i].name != want.Name || got[i].description != want.Description || got[i].filePath != want.FilePath {
			t.Errorf("skill %d = %+v, want %+v", i, got[i], want)
		}
	}
	t.Logf("diagnostics: pier=%d reference=%d", len(result.Diagnostics), len(fixture.Report.Diagnostics))
	for _, d := range result.Diagnostics {
		t.Logf("pier diagnostic: %s %s", d.Type, d.Message)
	}
}

func replacePrefix(value, prefix, replacement string) string {
	if len(value) >= len(prefix) && value[:len(prefix)] == prefix {
		return replacement + value[len(prefix):]
	}
	return value
}
