package ai

import (
	"strings"
	"testing"
)

type diagnosticCodeError struct{ code any }

func (e diagnosticCodeError) Error() string  { return "boom" }
func (e diagnosticCodeError) ErrorCode() any { return e.code }

// Diagnostics are persisted with JSON.stringify, which does not HTML-escape.
func TestDiagnosticCodeDoesNotHTMLEscape(t *testing.T) {
	info := ExtractDiagnosticError(diagnosticCodeError{code: "a < b & c > d"})
	if info.Code == nil {
		t.Fatal("code missing")
	}
	for _, escaped := range []string{`\u003c`, `\u0026`, `\u003e`} {
		if strings.Contains(string(info.Code), escaped) {
			t.Fatalf("diagnostic code HTML-escaped: %s", info.Code)
		}
	}
	if string(info.Code) != `"a < b & c > d"` {
		t.Fatalf("code = %s", info.Code)
	}
}
