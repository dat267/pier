package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// Reference: pi v1.5.0 (f9a26365),
// modes/interactive/components/settings-submenu.ts `SteppedSubmenu`, which
// the settings selector uses for the per-model thinking level. The step label,
// the context flowing into later steps, the completion snapshot and the
// back-step key deletion are the reference's own semantics.
func steppedSubmenuSteps(seenTitles *[]string) []SteppedSubmenuStep {
	return []SteppedSubmenuStep{
		{
			Key:         "model",
			Title:       func(map[string]string) string { return "Model" },
			Description: func(map[string]string) string { return "Pick a model" },
			Options: func(map[string]string) []tui.SelectItem {
				return []tui.SelectItem{{Value: "a", Label: "A"}, {Value: "b", Label: "B"}}
			},
		},
		{
			Key: "level",
			Title: func(context map[string]string) string {
				title := "Level for " + context["model"]
				*seenTitles = append(*seenTitles, title)
				return title
			},
			Description: func(map[string]string) string { return "Pick a level" },
			Options: func(map[string]string) []tui.SelectItem {
				return []tui.SelectItem{{Value: "high", Label: "high"}, {Value: "low", Label: "low"}}
			},
		},
	}
}

func TestSteppedSubmenuAdvancesWithContext(t *testing.T) {
	pinTUIKeybindings(t)
	InitTheme("dark", false)
	var titles []string
	var completed map[string]string
	cancels := 0
	submenu := NewSteppedSubmenu(steppedSubmenuSteps(&titles), func(selections map[string]string) {
		completed = selections
	}, func() { cancels++ }, SteppedSubmenuOptions{})

	// The first step carries its position and its description.
	first := strings.Join(submenu.Render(60), "\n")
	if !strings.Contains(first, "Step 1/2") || !strings.Contains(first, "Pick a model") {
		t.Fatalf("step 1 = %q, want the step label and description", first)
	}

	// Selecting advances, and the selection is visible to the next step.
	submenu.HandleInput("\r")
	if len(titles) != 1 || titles[0] != "Level for a" {
		t.Fatalf("step 2 titles = %q, want [Level for a]", titles)
	}
	second := strings.Join(submenu.Render(60), "\n")
	if !strings.Contains(second, "Step 2/2") {
		t.Fatalf("step 2 = %q, want the step label", second)
	}

	// The last step completes with every selection, then closes.
	submenu.HandleInput("\r")
	if completed["model"] != "a" || completed["level"] != "high" {
		t.Fatalf("completion = %v, want model a and level high", completed)
	}
	if cancels != 1 {
		t.Fatalf("cancel calls = %d, want 1 after a non-looping completion", cancels)
	}
}

func TestSteppedSubmenuLoopReturnsToTheFirstStep(t *testing.T) {
	pinTUIKeybindings(t)
	InitTheme("dark", false)
	var titles []string
	completions := 0
	submenu := NewSteppedSubmenu(steppedSubmenuSteps(&titles), func(map[string]string) {
		completions++
	}, func() {}, SteppedSubmenuOptions{Loop: true})

	submenu.HandleInput("\r")
	submenu.HandleInput("\r")
	if completions != 1 {
		t.Fatalf("completions = %d, want 1", completions)
	}
	if context := submenu.Context(); len(context) != 0 {
		t.Fatalf("context after a looping completion = %v, want empty", context)
	}
	if first := strings.Join(submenu.Render(60), "\n"); !strings.Contains(first, "Step 1/2") {
		t.Fatalf("after completion = %q, want step 1 again", first)
	}
}

func TestSteppedSubmenuBackDeletesTheStepKey(t *testing.T) {
	pinTUIKeybindings(t)
	InitTheme("dark", false)
	var titles []string
	var completed map[string]string
	cancels := 0
	submenu := NewSteppedSubmenu(steppedSubmenuSteps(&titles), func(selections map[string]string) {
		completed = selections
	}, func() { cancels++ }, SteppedSubmenuOptions{
		StartAtStep:    new(1),
		InitialContext: map[string]string{"model": "b", "level": "stale"},
	})

	// Backing out of the last step drops its key, so a later completion cannot
	// carry the stale value.
	submenu.HandleInput("\x1b")
	if context := submenu.Context(); context["level"] != "" {
		t.Fatalf("context after back = %v, want the level key dropped", context)
	}
	if first := strings.Join(submenu.Render(60), "\n"); !strings.Contains(first, "Step 1/2") {
		t.Fatalf("after back = %q, want step 1", first)
	}
	submenu.HandleInput("\r")
	submenu.HandleInput("\r")
	if completed["level"] != "high" {
		t.Fatalf("completion = %v, want the freshly chosen level", completed)
	}
	if cancels != 1 {
		t.Fatalf("cancel calls = %d, want 1", cancels)
	}
}

func TestSteppedSubmenuBackAtFirstStepCancels(t *testing.T) {
	pinTUIKeybindings(t)
	InitTheme("dark", false)
	var titles []string
	cancels := 0
	submenu := NewSteppedSubmenu(steppedSubmenuSteps(&titles), func(map[string]string) {}, func() { cancels++ }, SteppedSubmenuOptions{})
	submenu.HandleInput("\x1b")
	if cancels != 1 {
		t.Fatalf("cancel calls = %d, want 1", cancels)
	}
}
