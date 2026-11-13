package tui

// Replays the upstream slice goldens. They are generated from
// packages/tui/src/utils.ts by tui/testdata/slice/generate.mjs, which holds the
// same fifteen fixture lines; this file names each line by its index.
//
// The fixture covers what the functions' own tests cannot: every column of a
// line with wide characters, combining marks, SGR styling, OSC 8 links, a tab,
// an APC cursor marker and a kitty command, sliced at every boundary in both
// strict modes.

import (
	"encoding/json"
	"os"
	"testing"
)

var sliceGoldenLines = []string{
	"abcdef",
	"日本語",
	"a日b",
	"😀x",
	"e\u0301x",
	"\x1b[31mred\x1b[0m text",
	"\x1b]8;;https://a\x1b\\link\x1b]8;;\x1b\\ tail",
	"\x1b]8;;https://a\x1b\\日本\x1b]8;;\x1b\\x",
	"a\tb",
	"\x1b[31m\x1b]8;;u\x1b\\wide 日本\x1b]8;;\x1b\\\x1b[0m",
	"\x1b_Ga=T,f=100,i=7;AAAA\x1b\\",
	"\x1b_apc\x1b\\text",
	"👨‍👩‍👧x",
	"",
	"\x1b[31m",
}

func TestSliceLookupsMatchTheReference(t *testing.T) {
	data, err := os.ReadFile("testdata/slice/golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var cases []struct {
		Kind   string          `json:"kind"`
		Line   int             `json:"line"`
		Column int             `json:"column"`
		Start  int             `json:"start"`
		Length int             `json:"length"`
		Strict bool            `json:"strict"`
		Width  int             `json:"width"`
		Text   string          `json:"text"`
		Value  json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("golden is empty")
	}
	for _, testCase := range cases {
		if testCase.Line < 0 || testCase.Line >= len(sliceGoldenLines) {
			t.Fatalf("golden line index %d out of range", testCase.Line)
		}
		line := sliceGoldenLines[testCase.Line]
		label := func() string {
			encoded, _ := json.Marshal(testCase)
			return string(encoded)
		}
		switch testCase.Kind {
		case "width":
			if got := VisibleWidth(line); got != testCase.Width {
				t.Errorf("VisibleWidth(%q) = %d, reference gives %d", line, got, testCase.Width)
			}
		case "link":
			var want *string
			if err := json.Unmarshal(testCase.Value, &want); err != nil {
				t.Fatalf("%s: %v", label(), err)
			}
			got, ok := GetOsc8LinkAtColumn(line, testCase.Column)
			if want == nil {
				if ok {
					t.Errorf("GetOsc8LinkAtColumn(line %d, %d) = %q, reference gives none", testCase.Line, testCase.Column, got)
				}
				continue
			}
			if !ok || got != *want {
				t.Errorf("GetOsc8LinkAtColumn(line %d, %d) = %q (ok=%v), reference gives %q", testCase.Line, testCase.Column, got, ok, *want)
			}
		case "cell":
			var want *[2]int
			if err := json.Unmarshal(testCase.Value, &want); err != nil {
				t.Fatalf("%s: %v", label(), err)
			}
			got, ok := GetGraphemeCellRange(line, testCase.Column)
			if want == nil {
				if ok {
					t.Errorf("GetGraphemeCellRange(line %d, %d) = %+v, reference gives none", testCase.Line, testCase.Column, got)
				}
				continue
			}
			if !ok || got.Start != want[0] || got.End != want[1] {
				t.Errorf("GetGraphemeCellRange(line %d, %d) = %+v (ok=%v), reference gives %v", testCase.Line, testCase.Column, got, ok, *want)
			}
		case "slice":
			got := SliceWithWidth(line, testCase.Start, testCase.Length, testCase.Strict)
			if got.Text != testCase.Text || got.Width != testCase.Width {
				t.Errorf("SliceWithWidth(line %d, %d, %d, %v) = {%q %d}, reference gives {%q %d}",
					testCase.Line, testCase.Start, testCase.Length, testCase.Strict, got.Text, got.Width, testCase.Text, testCase.Width)
			}
		default:
			t.Fatalf("unknown golden kind %q", testCase.Kind)
		}
	}
}
