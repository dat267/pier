package coding

import (
	"encoding/json"
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

// AdoptLoopScheduler replaces the session's scheduler, but the scheduling tools
// were already registered, and registerLoopToolsLocked skips a name that exists.
// The registered tools therefore keep closures over the OLD scheduler, so a
// cron_list and a cron_delete can read different ones.
func TestAdoptLoopSchedulerRewiresRegisteredTools(t *testing.T) {
	first := NewLoopScheduler(nil, nil)
	defer first.Stop()
	second := NewLoopScheduler(nil, nil)
	defer second.Stop()

	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, ConvertToLlm: ConvertToLlm,
		Model:         &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000},
		Tools:         []agent.AgentTool{testTool("read")},
		LoopScheduler: first,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()

	session.AdoptLoopScheduler(second, LoopRecurringMaxAge)
	// A job on the scheduler the session now holds.
	job, err := second.Add("* * * * *", "tick", true, 0)
	if err != nil {
		t.Fatal(err)
	}

	// cron_list must see it, and cron_delete must be able to remove it.
	listResult := callToolByName(t, session, "cron_list", `{}`)
	if !strings.Contains(listResult, job.ID) {
		t.Fatalf("cron_list does not see the adopted scheduler's job %q:\n%s", job.ID, listResult)
	}
	deleteResult := callToolByName(t, session, "cron_delete", `{"id":"`+job.ID+`"}`)
	if strings.Contains(deleteResult, "No scheduled job") {
		t.Fatalf("cron_delete reads a different scheduler:\n%s", deleteResult)
	}
	if got := second.Jobs(); len(got) != 0 {
		t.Fatalf("the job survived: %d jobs on the adopted scheduler", len(got))
	}
}

// callToolByName runs one registered tool and returns its text.
func callToolByName(t *testing.T, session *AgentSession, name string, params string) string {
	t.Helper()
	session.control.stateMu.Lock()
	definition, ok := session.control.Tools[name]
	session.control.stateMu.Unlock()
	if !ok {
		t.Fatalf("tool %q is not registered", name)
	}
	result, err := definition.Tool.Execute(t.Context(), "call", json.RawMessage(params), nil)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var text strings.Builder
	for _, block := range result.Content {
		if value, ok := block.(ai.TextContent); ok {
			text.WriteString(value.Text)
		}
	}
	return text.String()
}

// cron_delete must act on the scheduler the session currently holds. When a
// rebind left jobs on the outgoing scheduler, the tool's captured Scheduler was
// the old one: it reported "No scheduled job" for a job it had just removed, and
// the old scheduler kept firing it.
func TestAdoptLoopSchedulerDrainsTheOutgoingOne(t *testing.T) {
	first := NewLoopScheduler(nil, nil)
	defer first.Stop()
	second := NewLoopScheduler(nil, nil)
	defer second.Stop()
	stale, err := first.Add("* * * * *", "stale", true, 0)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, ConvertToLlm: ConvertToLlm,
		Model:         &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000},
		Tools:         []agent.AgentTool{testTool("read")},
		LoopScheduler: first,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()

	session.AdoptLoopScheduler(second, LoopRecurringMaxAge)
	// The outgoing scheduler holds nothing, so no orphaned timer keeps firing.
	if got := len(first.Jobs()); got != 0 {
		t.Fatalf("adopting left %d jobs on the outgoing scheduler", got)
	}
	// And the tool acts on the new one: a job it can see, it can cancel.
	job, err := second.Add("* * * * *", "live", true, 0)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	result := callToolByName(t, session, "cron_delete", `{"id":"`+job.ID+`"}`)
	if strings.Contains(result, "No scheduled job") {
		t.Fatalf("cron_delete reported the job missing while removing it: %q", result)
	}
	if got := len(second.Jobs()); got != 0 {
		t.Fatalf("the job survived on the live scheduler: %d jobs", got)
	}
	// A genuinely unknown id still reports missing, which is what makes the
	// message useful.
	if got := callToolByName(t, session, "cron_delete", `{"id":"nope"}`); !strings.Contains(got, "No scheduled job") {
		t.Fatalf("an unknown id did not report missing: %q", got)
	}
	_ = stale
}
