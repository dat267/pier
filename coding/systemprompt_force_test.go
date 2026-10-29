package coding

import "testing"

// pi v1.4.0 system-prompt.ts treats any forceSystemPrompt other than undefined as a replacement.
func TestBuildSystemPromptForcedEmpty(t *testing.T) {
	content, sections, err := BuildSystemPromptState(BuildSystemPromptOptions{
		Cwd: "/p", ForceSystemPromptSet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if content != "" || sections != nil {
		t.Fatalf("empty forced state = %q, %v", content, sections)
	}
}
