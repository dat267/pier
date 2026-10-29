package coding

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/agent-session.ts _preparePromptAndToolLoadout:
// sections carry the builder's actual order into the transcript and requests.
func TestPromptKeepsNewBuiltinNamesInCustomOrder(t *testing.T) {
	var mu sync.Mutex
	var prompts []string
	session := newStablePromptSession(t, &prompts, &mu)
	session.SystemPromptOptions.CustomPrompt = "custom"
	session.SystemPromptOptions.Sections = map[string]string{"extra": "X", "tools": "T"}
	session.SystemPromptOptions.CustomSectionOrder = []string{"extra", "tools"}
	if err := session.PromptText(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	current := ai.GetCurrentSystemMessage(session.Messages())
	if current == nil {
		t.Fatal("no current system message")
	}
	if got := current.SectionOrder(); !reflect.DeepEqual(got, []string{"preamble", "cwd", "extra", "tools"}) {
		t.Fatalf("transcript section order = %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 1 || !strings.Contains(prompts[0], "</cwd>\n\n<extra>\nX\n</extra>\n\n<tools>\nT\n</tools>") {
		t.Fatalf("provider prompts = %q", prompts)
	}
}

// pi v1.4.0 (4a080252), core/system-prompt.ts buildSystemPromptSections:
// custom names retain base positions only when the base actually emitted them.
func TestBuildSystemPromptNewBuiltinNamesFollowCustomOrder(t *testing.T) {
	for _, test := range []struct {
		name    string
		options BuildSystemPromptOptions
		want    []string
	}{
		{
			name: "tools absent with custom preamble",
			options: BuildSystemPromptOptions{
				Cwd: "/work", CustomPrompt: "custom",
				Sections:           map[string]string{"extra": "X", "tools": "T"},
				CustomSectionOrder: []string{"extra", "tools"},
			},
			want: []string{"cwd", "extra", "tools"},
		},
		{
			name: "new addendum",
			options: BuildSystemPromptOptions{
				Cwd: "/work", Sections: map[string]string{"extra": "X", "addendum": "A"},
				CustomSectionOrder: []string{"extra", "addendum"},
			},
			want: []string{"tools", "rules", "cwd", "extra", "addendum"},
		},
		{
			name: "existing addendum override",
			options: BuildSystemPromptOptions{
				Cwd: "/work", AppendSystemPrompt: "base",
				Sections:           map[string]string{"extra": "X", "addendum": "A"},
				CustomSectionOrder: []string{"extra", "addendum"},
			},
			want: []string{"tools", "rules", "addendum", "cwd", "extra"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			prompt, err := BuildSystemPrompt(test.options)
			if err != nil {
				t.Fatal(err)
			}
			last := -1
			for _, name := range test.want {
				pos := strings.Index(prompt, "<"+name+">")
				if pos <= last {
					t.Fatalf("section %q at %d, want after %d in order %v: %q", name, pos, last, test.want, prompt)
				}
				last = pos
			}
		})
	}
}
