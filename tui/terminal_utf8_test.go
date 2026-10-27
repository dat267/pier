package tui

import (
	"fmt"
	"reflect"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), terminal.ts start uses stdin.setEncoding
// ("utf8") before feeding StdinBuffer. OS reads may split a UTF-8 character;
// neither fragment is a legacy high-byte Alt key. D49 keeps astral runes whole.
func TestProcessTerminalReassemblesUTF8Input(t *testing.T) {
	for _, text := range []string{"é", "界", "🙂"} {
		for split := 1; split < len(text); split++ {
			t.Run(fmt.Sprintf("%U/split%d", []rune(text)[0], split), func(t *testing.T) {
				terminal, _ := newRestartTestTerminal(t)
				terminal.EnableRawInput()
				terminal.Start(func(string) {}, nil)
				defer terminal.Stop()
				if got := terminal.FeedInput([]byte(text[:split])); len(got) != 0 {
					t.Errorf("incomplete UTF-8 was dispatched: %q", got)
				}
				if got := terminal.FeedInput([]byte(text[split:])); !reflect.DeepEqual(got, []string{text}) {
					t.Errorf("completed UTF-8 = %q, want [%q]", got, text)
				}
			})
		}
	}
}
