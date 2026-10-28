package tui

import (
	"reflect"
	"testing"
)

// pi v1.4.0 (4a080252), packages/tui/src/stdin-buffer.ts advances sequence
// candidates in UTF-16 characters. A BMP character after ESC/SS3 must not be
// split into UTF-8 bytes. D49 separately keeps astral Go runes whole.
func TestStdinBufferPreservesUnicodeEscapeCharacters(t *testing.T) {
	for _, c := range []struct {
		name, input string
		want        []string
	}{
		{"meta Latin", "\x1bé", []string{"\x1bé"}},
		{"meta CJK with tail", "\x1b中x", []string{"\x1b中", "x"}},
		{"SS3 with tail", "\x1bOéx", []string{"\x1bOé", "x"}},
		{"meta combining", "\x1b\u0301x", []string{"\x1b\u0301", "x"}},
		{"ASCII control", "\x1bax", []string{"\x1ba", "x"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			buffer := NewStdinBuffer(StdinBufferOptions{})
			defer buffer.Destroy()
			var got []string
			buffer.OnData = func(sequence string) { got = append(got, sequence) }
			buffer.ProcessString(c.input)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("sequences = %q, want %q", got, c.want)
			}
		})
	}
}
