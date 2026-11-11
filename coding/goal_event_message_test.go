package coding

import (
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// Upstream sendCustomMessage builds {role, customType, content, display,
// details, timestamp: Date.now()} (agent-session.ts v1.5.2 b4afdddf). A goal
// message enters the agent transcript in that shape, stamped, so the agent's
// copy matches the message a reload builds from its session entry.
func TestGoalEventMessageIsStampedInTheUpstreamCustomShape(t *testing.T) {
	message := goalEventMessage("goal-event", "keep going", true, json.RawMessage(`{"n":1}`))
	if message.Timestamp == 0 {
		t.Fatal("the goal message carries no timestamp")
	}
	got, err := ai.MarshalJSON(message)
	if err != nil {
		t.Fatal(err)
	}
	want, err := ai.MarshalJSON(CreateCustomMessage("goal-event", json.RawMessage(`"keep going"`), true, json.RawMessage(`{"n":1}`), message.Timestamp))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("goal message\n got %s\nwant %s", got, want)
	}
}
