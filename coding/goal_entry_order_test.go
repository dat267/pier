package coding

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dat267/pier/internal/goal"
)

// D223: entry_appended feeds the live transcript in durable append order.
// Reference: pi 4a080252 core/agent-session.ts entry_appended delivery. Session
// listeners are public callbacks and may reenter tools without a retained lock.
func TestGoalEntryEventsKeepAppendOrderOnReentry(t *testing.T) {
	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, Goal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	tool := session.GetToolDefinition("set_goal")
	set := func(objective string) {
		t.Helper()
		params, err := json.Marshal(map[string]string{"objective": objective})
		if err != nil {
			t.Fatal(err)
		}
		result, err := tool.Execute("call", params, context.Background(), nil)
		if err != nil || result.IsError {
			t.Fatalf("set_goal: result=%+v err=%v", result, err)
		}
	}
	var emitted []string
	reentered := false
	session.Subscribe(func(event *SessionEvent) {
		if event.Type != SessionEntryAppended || event.Entry == nil || event.Entry.CustomType != goal.GoalCustomType {
			return
		}
		var change goal.GoalChangeEntry
		if err := json.Unmarshal(event.Entry.Data, &change); err != nil {
			t.Fatal(err)
		}
		if change.Operation == goal.OpClear && !reentered {
			reentered = true
			set("third objective")
		}
	})
	// A second listener must see the same order as the session log, even when
	// an earlier subscriber reenters during the clear/create replacement pair.
	session.Subscribe(func(event *SessionEvent) {
		if event.Type == SessionEntryAppended && event.Entry != nil && event.Entry.CustomType == goal.GoalCustomType {
			emitted = append(emitted, event.Entry.ID)
		}
	})
	set("first objective")
	set("second objective")
	if !reentered {
		t.Fatal("listener never reentered set_goal")
	}
	var persisted []string
	for _, entry := range manager.GetBranch("") {
		if entry.Type == "custom" && entry.CustomType == goal.GoalCustomType {
			persisted = append(persisted, entry.ID)
		}
	}
	if len(persisted) != 5 || !reflect.DeepEqual(emitted, persisted) {
		t.Fatalf("event order differs from durable order:\n emitted: %v\n durable: %v", emitted, persisted)
	}
}
