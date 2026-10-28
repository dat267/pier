package highlight

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// pi v1.4.0 (4a080252), highlight.js core.js langRe always uses multiline
// JavaScript regular expressions. The Node-generated fixture also preserves
// literal line terminators, capture offsets and class-vs-dot distinctions.
func TestJavaScriptLineBoundaries(t *testing.T) {
	var fixture struct {
		Regex []struct {
			Name, Source, Input, Flags string
			Indices                    []int
		}
		Highlights []struct{ Name, Language, Code, HTML string }
	}
	data, err := os.ReadFile("testdata/line-boundaries/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Regex {
		t.Run("regex/"+tc.Name, func(t *testing.T) {
			re, err := compilePattern(tc.Source, tc.Flags)
			if err != nil {
				t.Fatal(err)
			}
			encoded := encodeCodeUnitsString(tc.Input)
			match := re.FindStringSubmatchIndex(encoded)
			var got []int
			for _, offset := range match {
				if offset < 0 {
					got = append(got, -1)
					continue
				}
				got = append(got, decodedByteOffset(encoded, offset))
			}
			if !reflect.DeepEqual(got, tc.Indices) {
				t.Fatalf("matches = %v; want %v", got, tc.Indices)
			}
		})
	}
	for _, tc := range fixture.Highlights {
		t.Run("highlight/"+tc.Name, func(t *testing.T) {
			result, err := Highlight(tc.Code, tc.Language)
			if err != nil {
				t.Fatal(err)
			}
			if got := RenderHTML(result.Nodes); got != tc.HTML {
				t.Fatalf("HTML = %q; want %q", got, tc.HTML)
			}
		})
	}
}

// A compiled language is shared across concurrent highlights; the rewritten
// program is immutable and each call owns its matching storage.
func TestJavaScriptLineBoundariesConcurrent(t *testing.T) {
	code := strings.Repeat("var caf\u00e9 = 1\r\n// note\u2028package demo\r\n", 20)
	want, err := Highlight(code, "go")
	if err != nil {
		t.Fatal(err)
	}
	wantHTML := RenderHTML(want.Nodes)
	done := make(chan string, 8)
	for range 8 {
		go func() {
			result, err := Highlight(code, "go")
			if err != nil {
				done <- err.Error()
				return
			}
			done <- RenderHTML(result.Nodes)
		}()
	}
	for range 8 {
		if got := <-done; got != wantHTML {
			t.Fatalf("concurrent highlight differs: %q", got)
		}
	}
}

func BenchmarkHighlightLineBoundaries(b *testing.B) {
	for _, ending := range []string{"\n", "\r\n"} {
		b.Run(strings.ReplaceAll(strings.ReplaceAll(ending, "\r", "CR"), "\n", "LF"), func(b *testing.B) {
			code := strings.Repeat("// comment"+ending+"package demo"+ending+"var value = 42"+ending, 100)
			if _, err := Highlight(code, "go"); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(code)))
			b.ResetTimer()
			for b.Loop() {
				if _, err := Highlight(code, "go"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
