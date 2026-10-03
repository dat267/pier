package durable

import "github.com/dat267/pier/chord"

// Port of harness/json.ts: assign a value leaf by leaf, so a partial write does
// not store and publish a whole container.

// AssignJSON assigns value at target[key], descending into objects and arrays
// that already exist. A removed object key is deleted; an array that grows
// keeps its existing elements.
func AssignJSON(target map[string]any, key string, value chord.JsonValue) {
	target[key] = assignJSONValue(target[key], value)
}

// assignJSONValue reconciles current with value and returns the value to store.
func assignJSONValue(current chord.JsonValue, value chord.JsonValue) chord.JsonValue {
	if currentRecord, ok := current.(map[string]any); ok {
		if valueRecord, ok := value.(map[string]any); ok {
			for name := range currentRecord {
				if _, present := valueRecord[name]; !present {
					delete(currentRecord, name)
				}
			}
			for name, child := range valueRecord {
				currentRecord[name] = assignJSONValue(currentRecord[name], child)
			}
			return currentRecord
		}
	}
	if currentArray, ok := current.([]any); ok {
		if valueArray, ok := value.([]any); ok && len(currentArray) <= len(valueArray) {
			result := currentArray
			for index := 0; index < len(valueArray); index++ {
				if index < len(result) {
					result[index] = assignJSONValue(result[index], valueArray[index])
				} else {
					result = append(result, valueArray[index])
				}
			}
			return result
		}
	}
	// Containers always replace (upstream compares by reference); primitives
	// compare by value.
	switch current.(type) {
	case map[string]any, []any:
		return value
	}
	if current == value {
		return current
	}
	return value
}
