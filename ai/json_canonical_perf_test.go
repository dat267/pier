package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// A long conversation body is canonicalized once per request and session
// rewrite; the nested value buffers must stay bounded (M143).
func canonicalProbeJSON(b testing.TB) []byte {
	b.Helper()
	messages := make([]map[string]any, 0, 4000)
	for i := 0; i < 4000; i++ {
		messages = append(messages, map[string]any{
			"role":    "user",
			"content": "question with <html> & unicode café 😀 and \"quotes\" and \\ backslash",
			"nested":  map[string]any{"z": i, "2": "two", "1": "one"},
			"list":    []any{i, "x", true, nil, 1.5},
		})
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		b.Fatal(err)
	}
	return encoded
}

func TestCanonicalizeLargeJSONAllocationBudget(t *testing.T) {
	encoded := canonicalProbeJSON(t)
	allocs := testing.AllocsPerRun(1, func() {
		if _, err := canonicalJavaScriptJSON(encoded); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 200000 {
		t.Fatalf("canonicalized %d bytes with %.0f allocations, want <= 200000", len(encoded), allocs)
	}
}

// A 4,000-message conversation body, the shape a late compaction or request
// marshals. Compare with Node JSON.stringify (about 1.1 ms for these bytes).
func BenchmarkMarshalConversation(b *testing.B) {
	messages := make([]Message, 0, 4000)
	for i := 0; i < 2000; i++ {
		messages = append(messages, &UserMessage{Content: StringOrBlocks{Text: "question with <html> & unicode café 😀 and \"quotes\" and \\ backslash"}})
		messages = append(messages, &AssistantMessage{Content: ContentList{TextContent{Text: "answer with <html> & unicode café 😀 and \"quotes\""}}, StopReason: StopStop})
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := MarshalJSON(messages); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanonicalizeLargeJSON(b *testing.B) {
	encoded := canonicalProbeJSON(b)
	b.SetBytes(int64(len(encoded)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := canonicalJavaScriptJSON(encoded); err != nil {
			b.Fatal(err)
		}
	}
}

// The optimized canonicalizer must produce the same bytes (or the same
// success/failure outcome) as the decoder-based reference.
func TestCanonicalizeMatchesReference(t *testing.T) {
	inputs := []string{
		`null`, `true`, `false`, `0`, `-0`, `1`, `1.5`, `1e2`, `1e-7`, `1e21`, `123456789012345678901234567890`,
		`"plain"`, `"quote \" backslash \\ tab \t control \u0001"`, `"\u2028\u2029"`, `"café 😀"`,
		`[]`, `{}`, `{"b":1,"a":2}`, `{"10":1,"2":2,"b":3,"1":4}`,
		`{"a":1,"a":2}`, `{"b":{"d":4,"c":3},"a":[1,{"z":0,"y":1}]}`,
		`[1,2,3]`, `[{"n":1},{"n":2}]`, `{"":1}`, `{"01":1,"1":2}`, `{"-1":1,"1":2}`,
		`{"4294967295":1,"4294967294":2}`, `{"a":"\ud800"}`, "\"\xff\"",
		`"\ud800\u0041"`, `"\ud800x"`, `"\ud800\udc00"`, `"\u007f"`, `"<&>"`,
		`01`, `truex`, `1abc`, `  `, ``,
		`1.`, `1e`, `-`, `tru`, `"\x"`, `[1,]`, `{"a":1,}`, `{"a" 1}`, `[1 2]`, `{"a":}`, `nul`,
	}
	for _, input := range inputs {
		want, wantErr := canonicalJSONDecoderFallback([]byte(input))
		got, gotErr := canonicalJavaScriptJSON([]byte(input))
		if wantErr != nil {
			if gotErr == nil {
				t.Fatalf("input %q: reference failed (%v) but got %q", input, wantErr, got)
			}
			continue
		}
		if gotErr != nil {
			t.Fatalf("input %q: %v; reference produced %q", input, gotErr, want)
		}
		if string(got) != string(want) {
			t.Fatalf("input %q: got %q, want %q", input, got, want)
		}
	}
	for _, size := range []int{8, 64, 512} {
		var builder strings.Builder
		for builder.Len() < size {
			builder.WriteString(`{"b":[1,{"z":"\u2028","a":1.5}],"2":null,"a":"café 😀"},`)
		}
		input := "[" + strings.TrimSuffix(builder.String(), ",") + "]"
		got, err := canonicalJavaScriptJSON([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		want, err := canonicalJSONDecoderFallback([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("size %d: got %q, want %q", size, got, want)
		}
	}
}

func FuzzCanonicalizeMatchesReference(f *testing.F) {
	for _, seed := range []string{`{"b":1,"a":[true,null,"\u2028"]}`, `01`, `"\ud800"`, "\"\xff\"", `[1,{"2":3}]`, `1e-7`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 512 {
			return
		}
		want, wantErr := canonicalJSONDecoderFallback(input)
		got, gotErr := canonicalJavaScriptJSON(input)
		if wantErr != nil {
			if gotErr == nil {
				t.Fatalf("input %q: reference failed (%v) but got %q", input, wantErr, got)
			}
			return
		}
		if gotErr != nil {
			t.Fatalf("input %q: %v; reference produced %q", input, gotErr, want)
		}
		if string(got) != string(want) {
			t.Fatalf("input %q: got %q, want %q", input, got, want)
		}
	})
}

// The decoder reference rejects nesting deeper than encoding/json's limit
// (10,000); accepting it would recurse until the stack fails on hostile input.
func TestCanonicalizeRejectsExcessiveNesting(t *testing.T) {
	allowed := strings.Repeat("[", 10000) + strings.Repeat("]", 10000)
	if _, err := canonicalJavaScriptJSON([]byte(allowed)); err != nil {
		t.Fatalf("depth 10000: %v", err)
	}
	rejected := strings.Repeat("[", 10001) + strings.Repeat("]", 10001)
	if _, err := canonicalJavaScriptJSON([]byte(rejected)); err == nil {
		t.Fatal("depth 10001 was accepted")
	}
}
