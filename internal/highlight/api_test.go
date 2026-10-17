package highlight

import "testing"

func TestSupportsLanguage(t *testing.T) {
	for _, name := range []string{"go", "golang", "typescript", "ts", "xml"} {
		if !SupportsLanguage(name) {
			t.Errorf("SupportsLanguage(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "not-a-real-language"} {
		if SupportsLanguage(name) {
			t.Errorf("SupportsLanguage(%q) = true, want false", name)
		}
	}
}
