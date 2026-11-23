package tui

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
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
		return strconv.Itoa(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, stableMDValue(item))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]any:
		keys := slices.Sorted(maps.Keys(typed))
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
