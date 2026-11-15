package coding

import (
	"context"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.5.2 (b4afdddf) cli/auth-login.ts selectInTerminal accepts an
// answer only when /^\d+$/u matches the whole trimmed line; pier scanned a
// leading integer, so "1 2" or "1abc" selected the first option instead of
// re-prompting.
func TestSelectInTerminalRequiresTheWholeLineToBeADigits(t *testing.T) {
	terminal := &scriptedLoginTerminal{answers: []string{"1 2", "2"}}
	options := []ai.AuthSelectOption{{ID: "first", Label: "First"}, {ID: "second", Label: "Second"}}
	choice, err := selectInTerminal(context.Background(), "Pick:", options, terminal)
	if err != nil {
		t.Fatal(err)
	}
	if choice != "second" {
		t.Fatalf("choice = %q, want the second option after re-prompting", choice)
	}
	if !strings.Contains(terminal.output(), "Enter a number from 1 to 2.") {
		t.Fatalf("output = %q", terminal.output())
	}
}
