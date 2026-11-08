package ai

import (
	"strconv"
	"strings"
	"testing"
)

// numericKeyDoc builds a one-level object of n members, either with named
// keys ("k0", "k1", ...) or with decimal index keys in ascending or
// descending order, the shapes JS orders differently.
func numericKeyDoc(n int, numeric, descending bool) string {
	var builder strings.Builder
	builder.WriteString("{")
	for i := range n {
		if i > 0 {
			builder.WriteString(",")
		}
		key := "k" + strconv.Itoa(i)
		if numeric {
			value := i
			if descending {
				value = n - i
			}
			key = strconv.Itoa(value)
		}
		builder.WriteString(`"` + key + `":` + strconv.Itoa(i))
	}
	builder.WriteString("}")
	return builder.String()
}

// BenchmarkStreamingArgumentsKeyShapes measures a streamed tool call's
// arguments at 16-byte deltas for the three member shapes A424 distinguishes:
// named keys (an ordinary call), ascending index keys, and descending index
// keys. The last two are quadratic today: memberPosition rescans the frame for
// an index key, and a key that sorts before existing members is spliced by
// copying every following member (see TODO.md A424). The reference
// (pi v1.5.0 packages/ai/src/utils/streaming-json.ts) inserts into a JS object
// in O(1) amortized time, so the numeric shapes should approach the named one.
func BenchmarkStreamingArgumentsKeyShapes(b *testing.B) {
	for _, shape := range []struct {
		name       string
		numeric    bool
		descending bool
	}{
		{"named", false, false},
		{"index-ascending", true, false},
		{"index-descending", true, true},
	} {
		doc := numericKeyDoc(2000, shape.numeric, shape.descending)
		b.Run(shape.name, func(b *testing.B) {
			b.SetBytes(int64(len(doc)))
			for b.Loop() {
				var args streamingArguments
				for rest := doc; rest != ""; {
					size := min(len(rest), 16)
					args.Append(rest[:size])
					rest = rest[size:]
				}
			}
		})
	}
}
