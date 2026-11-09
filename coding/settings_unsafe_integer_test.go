package coding

import (
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// The compaction token settings are validated against JavaScript's safe-integer
// range: pi v1.5.0 (f9a26365) core/settings-manager.ts getCompactionTokenSetting
// rejects a value with `!Number.isSafeInteger(ordinary) || ordinary < 0`.
// Number.isSafeInteger is false above 2^53-1, where a JavaScript number can no
// longer hold every integer, so the reference refuses such a setting instead of
// letting it round. The port checked only `< 0`, so a value past that range was
// accepted: 2^53+1 arrived as 2^53 from the JSON decoder and was used.
func TestCompactionTokenSettingsRejectUnsafeIntegers(t *testing.T) {
	cases := []struct {
		name    string
		content string
		model   *ai.Model
	}{
		{"reserveTokens", `{"compaction":{"reserveTokens":9007199254740993}}`, nil},
		{"keepRecentTokens", `{"compaction":{"keepRecentTokens":9007199254740993}}`, nil},
		{
			name:    "model override",
			content: `{"compaction":{"modelOverrides":{"anthropic/m":{"reserveTokens":9007199254740993}}}}`,
			model:   &ai.Model{Provider: "anthropic", ID: "m"},
		},
		{
			// 2^53-1 is Number.MAX_SAFE_INTEGER: the reference accepts it.
			name:    "model override at the limit",
			content: `{"compaction":{"modelOverrides":{"anthropic/m":{"reserveTokens":9007199254740991}}}}`,
			model:   &ai.Model{Provider: "anthropic", ID: "m"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var raw map[string]any
			if err := json.Unmarshal([]byte(testCase.content), &raw); err != nil {
				t.Fatal(err)
			}
			_ = MigrateSettingsRaw(raw)
			manager := NewInMemorySettingsManager(mapToSettings(raw), SettingsManagerCreateOptions{})
			_, reserveErr := manager.GetCompactionReserveTokens(testCase.model)
			_, keepErr := manager.GetCompactionKeepRecentTokens(testCase.model)
			err := reserveErr
			if err == nil {
				err = keepErr
			}
			atLimit := testCase.name == "model override at the limit"
			if atLimit && err != nil {
				t.Fatalf("2^53 was rejected: %v", err)
			}
			if !atLimit && err == nil {
				t.Fatal("an unsafe integer was accepted")
			}
		})
	}
}
