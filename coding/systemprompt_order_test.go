package coding

import (
	"slices"
	"strings"
	"testing"
)

// pi v1.4.0 system-prompt.ts renders custom sections via Object.entries in insertion order.
func TestPreparePromptAndToolLoadoutPreservesCustomSectionOrder(t *testing.T) {
	session := newControlSession(t, nil, nil)
	session.SystemPromptOptions.Sections = map[string]string{"alpha": "A", "zeta": "Z"}
	session.SystemPromptOptions.CustomSectionOrder = []string{"zeta", "alpha"}

	message := session.preparePromptAndToolLoadout()
	if message == nil {
		t.Fatal("expected a system-message patch")
	}
	order := message.SectionOrder()
	if slices.Index(order, "zeta") < 0 || slices.Index(order, "zeta") > slices.Index(order, "alpha") {
		t.Fatalf("custom sections not in supplied patch order: %v", order)
	}
}

func TestBuildSystemPromptPreservesCustomSectionOrder(t *testing.T) {
	prompt, err := BuildSystemPrompt(BuildSystemPromptOptions{
		Cwd:                "/p",
		Sections:           map[string]string{"alpha": "A", "zeta": "Z"},
		CustomSectionOrder: []string{"zeta", "alpha"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(prompt, "<zeta>") < 0 || strings.Index(prompt, "<zeta>") > strings.Index(prompt, "<alpha>") {
		t.Fatalf("custom sections not in supplied order: %q", prompt)
	}

	prompt, err = BuildSystemPrompt(BuildSystemPromptOptions{
		Cwd:      "/p",
		Sections: map[string]string{"zeta": "Z", "alpha": "A"},
	})
	if err != nil {
		t.Fatal(err)
	}
	alpha, zeta := strings.Index(prompt, "<alpha>"), strings.Index(prompt, "<zeta>")
	if alpha < 0 || zeta < 0 || alpha > zeta {
		t.Fatalf("unordered custom sections not sorted deterministically: %q", prompt)
	}
}
