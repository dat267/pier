package coding

import (
	"encoding/json"
	"testing"

	"github.com/dat267/pier/internal/goal"
)

// D223 ports goal/machine.ts's resume lifecycle. The local phase graph in
// internal/goal/state.go makes complete terminal, so rejecting /goal resume
// must leave both the durable goal and its process-local activation unchanged.
// The extension is not part of pinned pi 4a080252 and is unavailable here.
func TestGoalResumeLeavesCompletedGoalDisarmed(t *testing.T) {
	controller, sink := newGoalControllerTest(t)
	goalToolText(t, controller, "set_goal", `{"objective":"finish it"}`)
	goalToolText(t, controller, "set_goal", `{"status":"complete"}`)
	before := goalToolText(t, controller, "get_goal", `{}`)
	entries, messages := len(sink.entries), len(sink.messages)

	controller.HandleCommand("resume")

	var view goal.GoalViewResult
	after := goalToolText(t, controller, "get_goal", `{}`)
	if err := json.Unmarshal([]byte(after), &view); err != nil {
		t.Fatal(err)
	}
	if view.Goal == nil || view.Goal.Phase != goal.PhaseComplete || view.Activation != "disarmed" {
		t.Fatalf("resume changed terminal goal: %s", after)
	}
	if after != before {
		t.Fatalf("rejected resume changed goal\nbefore: %s\nafter: %s", before, after)
	}
	if len(sink.entries) != entries || len(sink.messages) != messages || sink.runs != 0 {
		t.Fatalf("rejected resume delivered work: entries=%d messages=%d runs=%d", len(sink.entries)-entries, len(sink.messages)-messages, sink.runs)
	}
}
