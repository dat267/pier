package coding

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// pi v1.4.0 (4a080252), core/agent-session.ts _preparePromptAndToolLoadout:
// a custom section omitted from the rebuilt prompt is removed by the next patch.
func TestPromptRemovesCustomSectionWhenContentBecomesEmpty(t *testing.T) {
	var mu sync.Mutex
	var prompts []string
	session := newStablePromptSession(t, &prompts, &mu)
	session.SystemPromptOptions.Sections = map[string]string{"extra": "Enabled"}
	if err := session.PromptText(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	session.SystemPromptOptions.Sections = map[string]string{"extra": ""}
	if err := session.PromptText(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 2 {
		t.Fatalf("captured %d requests, want 2", len(prompts))
	}
	if !strings.Contains(prompts[0], "<extra>\nEnabled\n</extra>") {
		t.Fatalf("first request missing enabled section: %q", prompts[0])
	}
	if strings.Contains(prompts[1], "<extra>") {
		t.Fatalf("second request retains disabled section: %q", prompts[1])
	}
}

// pi v1.4.0 (4a080252), packages/coding-agent/src/core/system-prompt.ts:
// buildSystemPromptSections merges only truthy custom section content.
func TestBuildSystemPromptOmitsEmptyCustomSections(t *testing.T) {
	for _, customPrompt := range []string{"", "Custom prompt"} {
		for _, name := range []string{"extra", "addendum", "project_context", "skills"} {
			t.Run(customPrompt+"/"+name, func(t *testing.T) {
				for _, content := range []string{"", " ", "text"} {
					sections, err := BuildSystemPromptSections(BuildSystemPromptOptions{
						Cwd: "/work", CustomPrompt: customPrompt,
						Sections: map[string]string{name: content},
					})
					if err != nil {
						t.Fatal(err)
					}
					value, present := sections[name]
					if present != (content != "") {
						t.Errorf("content %q: present = %v, want %v (section %q)", content, present, content != "", value)
					}
					if content != "" && value != "<"+name+">\n"+content+"\n</"+name+">" {
						t.Errorf("content %q: wrapped section = %q", content, value)
					}
				}
			})
		}
	}
}
