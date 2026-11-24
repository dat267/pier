package coding

import (
	"strings"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// newLoopFeatureSession builds a session with the scheduling tools installed and
// a scheduler whose jobs can be inspected.
func newLoopFeatureSession(t *testing.T) (*AgentSession, *LoopScheduler) {
	t.Helper()
	manager, dir := newTestSession(t)
	scheduler := NewLoopScheduler(nil, nil)
	t.Cleanup(scheduler.Stop)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, ConvertToLlm: ConvertToLlm,
		Model:         &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000},
		Tools:         []agent.AgentTool{testTool("read")},
		LoopScheduler: scheduler,
		LoopMaxAge:    LoopRecurringMaxAge,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Dispose)
	return session, scheduler
}

// Turning the loop feature off removes the scheduling tools from the registry
// and the active set, and drops the jobs, so /loop cannot fire behind the user's
// back. Turning it back on restores the tools.
func TestLoopFeatureToggleEnablesAndDisables(t *testing.T) {
	session, scheduler := newLoopFeatureSession(t)
	hasTool := func(name string) bool {
		for _, candidate := range session.ToolNames() {
			if candidate == name {
				return true
			}
		}
		return false
	}
	active := func(name string) bool {
		for _, candidate := range session.GetActiveToolNames() {
			if candidate == name {
				return true
			}
		}
		return false
	}
	if !hasTool("cron_create") || !active("cron_create") {
		t.Fatal("the scheduling tools were not registered at startup")
	}
	if _, err := scheduler.Add("*/5 * * * *", "poll", true, 0); err != nil {
		t.Fatalf("Add: %v", err)
	}

	session.SetLoopFeatureEnabled(false)
	if hasTool("cron_create") || hasTool("cron_list") || hasTool("schedule_wakeup") {
		t.Fatalf("the scheduling tools stayed registered: %v", session.ToolNames())
	}
	if active("cron_create") {
		t.Fatal("cron_create stayed in the active set")
	}
	if jobs := scheduler.Jobs(); len(jobs) != 0 {
		t.Fatalf("jobs = %d after disabling the feature, want none", len(jobs))
	}

	session.SetLoopFeatureEnabled(true)
	if !hasTool("cron_create") || !active("cron_create") {
		t.Fatalf("turning the feature back on did not restore the tools: %v", session.ToolNames())
	}
}

// A user who turned one scheduling tool off in /tools does not get it back when
// the feature is toggled, the same rule the goal tools follow (D497).
func TestLoopFeatureToggleRespectsToolToggle(t *testing.T) {
	session, _ := newLoopFeatureSession(t)
	if err := session.SetToolEnabled("cron_list", false); err != nil {
		t.Fatalf("SetToolEnabled: %v", err)
	}
	session.SetLoopFeatureEnabled(false)
	session.SetLoopFeatureEnabled(true)
	for _, name := range session.GetActiveToolNames() {
		if name == "cron_list" {
			t.Fatalf("cron_list came back although /tools turned it off: %v", session.GetActiveToolNames())
		}
	}
	if !strings.Contains(strings.Join(session.ToolNames(), ","), "cron_list") {
		t.Fatal("cron_list left the registry entirely, so /tools can no longer show it")
	}
}
