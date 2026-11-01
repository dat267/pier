package interactive

import (
	"encoding/json"
	"testing"
)

// FuzzParseThemeJSON feeds untrusted theme files (project and agent theme
// directories) through validation and parsing in both modes: a panic here is a
// crash on `--theme` or a discovered theme.
func FuzzParseThemeJSON(f *testing.F) {
	for _, seed := range []string{
		`{}`, `{"name":"x"}`, `{"colors":{"text":"#fff"}}`, `{"colors":{"text":null}}`,
		`{"colors":{"text":{"light":"#fff","dark":"#000"}}}`,
		`{"colors":{"text":"oklch(0.5 0.1 20)"}}`, `[]`, `null`, `{"colors":[]}`, `{"exports":{"pageBg":"#fff"}}`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		_, _ = ParseThemeJSON("fuzz", json.RawMessage(raw), true)
		_, _ = ParseThemeJSON("fuzz", json.RawMessage(raw), false)
	})
}
