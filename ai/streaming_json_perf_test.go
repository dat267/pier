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
// keys, which JS orders before every member already read. D353 records the
// numbers against pi v1.5.0 packages/ai/src/utils/streaming-json.ts.
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

// A424: keys JS orders before earlier ones (descending and mixed index keys,
// a duplicate, a nested object) render in pi v1.5.0
// packages/ai/src/utils/streaming-json.ts order at every piece; the expected
// previews are that reference's output for these pieces.
func TestStreamingJSONOutOfOrderKeysMatchReference(t *testing.T) {
	fakeStreamingJSONClock(t, 1<<40)
	pieces := []string{`{"3":1,"b":{"2":"x`, `y","1":[1,{"9":2,"8":tr`, `ue}]},"1":4`, `5,"b":7,"0":{"z":1}}`}
	want := []string{
		`{"3":1,"b":{"2":"x"}}`,
		`{"3":1,"b":{"1":[1,{"8":true,"9":2}],"2":"xy"}}`,
		`{"1":4,"3":1,"b":{"1":[1,{"8":true,"9":2}],"2":"xy"}}`,
		`{"0":{"z":1},"1":45,"3":1,"b":7}`,
	}
	var args streamingArguments
	for i, piece := range pieces {
		if got := string(args.Append(piece)); got != want[i] {
			t.Fatalf("piece %d: preview %s, want %s", i, got, want[i])
		}
	}
	final, _ := args.Finish()
	if string(final) != want[len(want)-1] {
		t.Fatalf("Finish = %s", final)
	}
}

// A424: index keys arriving out of order cost no copy of the members already
// read, so streaming them allocates like named keys do (it was quadratic).
func TestStreamingArgumentsDescendingIndexKeysStayLinear(t *testing.T) {
	fakeStreamingJSONClock(t, 1)
	named := numericKeyDoc(2000, false, false)
	descending := numericKeyDoc(2000, true, true)
	allocs := func(doc string) float64 {
		return testing.AllocsPerRun(2, func() {
			var args streamingArguments
			for rest := doc; rest != ""; {
				size := min(len(rest), 16)
				args.Append(rest[:size])
				rest = rest[size:]
			}
			args.Finish()
		})
	}
	base, got := allocs(named), allocs(descending)
	if got > 2*base+64 {
		t.Fatalf("descending index keys: %.0f allocations, named keys %.0f", got, base)
	}
}
