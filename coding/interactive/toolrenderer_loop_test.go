package interactive

import (
	"encoding/json"
	"strings"
	"testing"
)

// The scheduled-prompt tools are pier-only, so they have no reference renderer.
// Without one they fell through to the generic MCP frame, which showed the raw
// argument JSON: a wall of {"cron": "...", "prompt": "..."} in the transcript.
func TestLoopToolRenderers(t *testing.T) {
	theme := newRendererTestTheme(t)

	// cron_create shows the schedule and the prompt, not the argument object.
	args, err := json.Marshal(map[string]any{
		"cron":      "*/1 * * * *",
		"prompt":    "Tell the user the current time using the bash `date` command.",
		"recurring": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	call := renderCall("cron_create", args, theme)
	for _, want := range []string{"cron_create", "Every minute", "current time"} {
		if !strings.Contains(call, want) {
			t.Errorf("cron_create call lacks %q:\n%s", want, call)
		}
	}
	for _, unwanted := range []string{`"cron"`, `{`, `}`} {
		if strings.Contains(call, unwanted) {
			t.Errorf("cron_create call still shows raw JSON (%q):\n%s", unwanted, call)
		}
	}

	// A one-shot job says so; a recurring one does not repeat the default.
	once, err := json.Marshal(map[string]any{
		"cron": "30 14 * * *", "prompt": "remind me", "recurring": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := renderCall("cron_create", once, theme); !strings.Contains(got, "(once)") {
		t.Errorf("a one-shot job is not labelled:\n%s", got)
	}
	recurring, err := json.Marshal(map[string]any{
		"cron": "30 14 * * *", "prompt": "remind me", "recurring": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := renderCall("cron_create", recurring, theme); strings.Contains(got, "(once)") {
		t.Errorf("a recurring job is labelled one-shot:\n%s", got)
	}

	// cron_delete names the job.
	deleteArgs, err := json.Marshal(map[string]any{"id": "3"})
	if err != nil {
		t.Fatal(err)
	}
	if got := renderCall("cron_delete", deleteArgs, theme); !strings.Contains(got, "job 3") {
		t.Errorf("cron_delete does not name the job:\n%s", got)
	}

	// cron_list: the call is just the name; the result is the job rows.
	if got := strings.TrimSpace(renderCall("cron_list", json.RawMessage(`{}`), theme)); got != "cron_list" {
		t.Errorf("cron_list call = %q", got)
	}
	listed := &SortToolResultContent{Content: []ToolResultContent{{
		Type: "text",
		Text: "1 — Every minute (recurring), next run 2026-11-25T05:08:00+07:00: tell the time",
	}}}
	rows := renderResult("cron_list", listed, false, theme)
	if !strings.Contains(rows, "Every minute") || !strings.Contains(rows, "tell the time") {
		t.Errorf("cron_list result lost the job:\n%s", rows)
	}

	// schedule_wakeup shows the delay, and "stop" for a stop.
	wakeup, err := json.Marshal(map[string]any{"delay_seconds": 270.0, "prompt": "/loop check"})
	if err != nil {
		t.Fatal(err)
	}
	if got := renderCall("schedule_wakeup", wakeup, theme); !strings.Contains(got, "4 minutes") {
		t.Errorf("schedule_wakeup does not show the delay:\n%s", got)
	}
	stop, err := json.Marshal(map[string]any{"stop": true})
	if err != nil {
		t.Fatal(err)
	}
	if got := renderCall("schedule_wakeup", stop, theme); !strings.Contains(got, "stop") {
		t.Errorf("a stop does not say so:\n%s", got)
	}
}

// The renderers are registered, or the fallback frame returns.
func TestLoopToolsHaveRenderers(t *testing.T) {
	for _, name := range []string{"cron_create", "cron_delete", "cron_list", "schedule_wakeup"} {
		if _, ok := builtinToolRenderers[name]; !ok {
			t.Errorf("%s has no built-in renderer, so it falls back to the MCP frame", name)
		}
	}
	names := BuiltinToolRendererNames()
	for _, want := range []string{"cron_create", "cron_delete", "cron_list", "schedule_wakeup"} {
		found := false
		for _, name := range names {
			if name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is not listed by BuiltinToolRendererNames", want)
		}
	}
}
