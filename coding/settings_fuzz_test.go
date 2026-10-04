package coding

import (
	"encoding/json"
	"reflect"
	"testing"
)

// FuzzSettingsCodec checks the settings codecs never panic on arbitrary JSON
// shapes and that a decoded settings round-trips through the writer.
func FuzzSettingsCodec(f *testing.F) {
	seeds := []string{
		`{}`,
		`{"theme":"dark","quietStartup":true}`,
		`{"modelThinkingLevels":{"openai/gpt-5":"high"}}`,
		`{"compaction":{"enabled":false}}`,
		`{"unknownKey":{"nested":[1,2,3]}}`,
		`{"theme":123}`,
		`{"fullscreenWheelScrollLines":"auto"}`,
		`{"httpIdleTimeoutMs":null}`,
		`{"images":{"autoResize":true},"markdown":{"mermaid":false}}`,
		`[]`,
		`null`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		var raw map[string]any
		if err := json.Unmarshal([]byte(content), &raw); err != nil {
			return
		}
		_ = MigrateSettingsRaw(raw)
		_ = migrateSettingsMap(raw)
		settings := mapToSettings(raw)
		mapped := settingsToMap(settings)
		_ = cloneSettings(settings)
		// The writer output must be valid JSON.
		if _, err := settings.marshalOrdered(); err != nil {
			t.Fatalf("marshalOrdered failed for %q: %v", content, err)
		}
		if mapped == nil {
			t.Fatalf("settingsToMap returned nil for %q", content)
		}
		// The typed view is the codec's fixed point: writing it and reading it
		// back must reproduce it exactly (a field written but not read, or read
		// with a different shape, is a codec asymmetry).
		if again := mapToSettings(mapped); !reflect.DeepEqual(settings, again) {
			t.Fatalf("settings codec is not idempotent for %q:\n first = %+v\n again = %+v", content, settings, again)
		}
	})
}
