package tui

import (
	"encoding/json"
	"strings"
)

// stableMDValue serializes with sorted keys, matching the JS stable() helper.
func stableMDValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		return mustJSON(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case int:
		return itoa(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, stableMDValue(item))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sortStrings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, mustJSON(key)+":"+stableMDValue(typed[key]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			panic(err)
		}
		return string(encoded)
	}
}
