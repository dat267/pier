package coding

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzLoadModelConfig checks the models.json loader and validator never panic on
// arbitrary file content.
func FuzzLoadModelConfig(f *testing.F) {
	seeds := []string{
		`{"providers":{"p":{"models":[{"id":"m","samplingParamsByThinkingLevel":{"low":{"top_p":0.5}}}]}}}`,
		`{"providers":{"p":{"modelOverrides":{"m":{"cost":{"input":1}}}}}}`,
		`{"providers":{"p":{"compat":{"thinkingFormat":"nope"}}}}`,
		"// comment\n{\"providers\":{}}",
		"{ not json",
		"[]",
		"\x00\xff",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		dir, err := os.MkdirTemp("", "pier-models-fuzz")
		if err != nil {
			t.Skip(err)
		}
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, "models.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Skip(err)
		}
		config := LoadModelConfig(path)
		_ = config.GetError()
		ids := config.GetProviderIDs()
		for _, id := range ids {
			provider := config.GetProvider(id)
			if provider == nil {
				continue
			}
			_, _ = applyModelsJSON(id, nil, provider)
		}
	})
}
