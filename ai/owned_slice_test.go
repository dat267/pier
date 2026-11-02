package ai

import (
	"strings"
	"testing"
	"unsafe"
)

// Reference: pi v1.5.0 utils/text.ts ownedSlice. Go substrings share the
// backing array, so a 500-byte error excerpt of a megabyte body would retain
// the whole body; the truncation sites must copy.
func TestTruncatedErrorTextOwnsItsBytes(t *testing.T) {
	body := strings.Repeat("x", 1<<20) + "tail"
	truncated := TruncateErrorText(body, 500)
	if !strings.HasPrefix(truncated, strings.Repeat("x", 500)) {
		t.Fatalf("truncated prefix = %q", truncated[:20])
	}
	if unsafe.StringData(truncated) == unsafe.StringData(body) {
		t.Fatal("TruncateErrorText returned a view into the full body")
	}

	diagnosticSource := strings.Repeat("y", 20000)
	diagnostic := truncateDiagnosticString(diagnosticSource)
	if unsafe.StringData(diagnostic) == unsafe.StringData(diagnosticSource) {
		t.Fatal("diagnostic truncation returned a view")
	}
	httpSource := strings.Repeat("z", 20000)
	if got := truncateHTTPBody(httpSource); unsafe.StringData(got) == unsafe.StringData(httpSource) {
		t.Fatal("HTTP body truncation returned a view")
	}
}
