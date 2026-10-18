package oauth

import "testing"

// safeUrl must reject the scripting schemes the reference blocks
// (types.ts: `["javascript:", "data:", "vbscript:"].includes(url.protocol)`).
// Go's url.Scheme drops the colon the WHATWG protocol keeps, so the comparison
// has to use the bare scheme.
func TestSafeURLRejectsScriptingSchemes(t *testing.T) {
	for _, value := range []string{
		"javascript:alert(1)",
		"JAVASCRIPT:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"vbscript:msgbox(1)",
	} {
		if _, err := safeUrl(map[string]any{"url": value}, "url", "endpoint"); err == nil {
			t.Errorf("safeUrl(%q) accepted a scripting scheme", value)
		}
	}
	if got, err := safeUrl(map[string]any{"url": "https://example.com/x"}, "url", "endpoint"); err != nil || got != "https://example.com/x" {
		t.Fatalf("safeUrl(https) = %q, %v", got, err)
	}
}
