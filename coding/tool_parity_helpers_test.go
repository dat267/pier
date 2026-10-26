package coding

// Shared helpers for the tool-level parity corpora, kept in one file so a
// corpus can be removed without taking another corpus's fixtures with it.

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func itoaForTest(value int) string { return strconv.Itoa(value) }

func manyLines(count int) string {
	var builder strings.Builder
	for index := 1; index <= count; index++ {
		builder.WriteString("line ")
		builder.WriteString(itoaForTest(index))
		builder.WriteString("\n")
	}
	return builder.String()
}

func canonicalReadJSON(t *testing.T, raw []byte) any {
	t.Helper()
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var sortKeys func(any) any
	sortKeys = func(input any) any {
		switch typed := input.(type) {
		case map[string]any:
			out := make(map[string]any, len(typed))
			for key, item := range typed {
				out[key] = sortKeys(item)
			}
			return out
		case []any:
			for index := range typed {
				typed[index] = sortKeys(typed[index])
			}
			return typed
		default:
			return input
		}
	}
	return sortKeys(value)
}

// D279: the reference surfaces Node's libuv wording for a missing file
// ("ENOENT: no such file or directory, access '<path>'"); the port keeps Go's
// os error text. This pins the port's message so the divergence stays visible.
