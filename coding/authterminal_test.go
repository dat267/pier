package coding

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// pi v1.5.2 (b4afdddf) utils/terminal-question.ts answers questions through
// node:readline/promises and aborts the pending question when readline closes, so
// end of input always rejects with "Login cancelled", whether or not a partial
// line arrived first. The port returned the partial line, which handed a login
// prompt a value the user never submitted.
func TestQuestionRejectsAtEndOfInputWithoutNewline(t *testing.T) {
	out := &bytes.Buffer{}
	terminal := NewProcessLoginTerminal(strings.NewReader("partial"), out, &bytes.Buffer{}, false, -1)
	answer, err := terminal.Question(context.Background(), "value: ")
	if err == nil {
		t.Fatalf("answer = %q, want the cancelled error at end of input", answer)
	}
	if !strings.Contains(err.Error(), AuthLoginCancelledMessage) {
		t.Fatalf("error = %v, want %q", err, AuthLoginCancelledMessage)
	}
}

// A complete line, terminated by a newline, is the only accepted input.
func TestQuestionReturnsATerminatedLine(t *testing.T) {
	terminal := NewProcessLoginTerminal(strings.NewReader("value\r\n"), &bytes.Buffer{}, &bytes.Buffer{}, false, -1)
	answer, err := terminal.Question(context.Background(), "value: ")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if answer != "value" {
		t.Fatalf("answer = %q, want %q", answer, "value")
	}
}
