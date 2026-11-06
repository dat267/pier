package agent

import (
	"context"
	"testing"

	"github.com/dat267/pier/ai"
)

// A deferred follow-up keeps its place for a later run while the rest of the
// queue is still delivered.
func TestDrainDeferringSkipsHeldMessages(t *testing.T) {
	isUser := func(message ai.Message) bool { return ai.RoleOf(message) == ai.RoleUser }
	for _, mode := range []QueueMode{QueueModeOneAtATime, QueueModeAll} {
		queue := &pendingMessageQueue{mode: mode}
		user := createUserMessage("held")
		round := &ai.CustomMessage{Role: "custom", Content: []byte(`{"customType":"pi-goal-event","content":""}`)}
		queue.enqueue(user)
		queue.enqueue(round)
		taken := queue.drainDeferring(isUser)
		if len(taken) != 1 || ai.RoleOf(taken[0]) != "custom" {
			t.Fatalf("%s: took %v, want only the goal round", mode, messageRoles(taken))
		}
		if queue.hasDeliverable(isUser) {
			t.Fatalf("%s: held message reported as deliverable", mode)
		}
		if rest := queue.takeAll(); len(rest) != 1 || ai.RoleOf(rest[0]) != ai.RoleUser {
			t.Fatalf("%s: takeAll = %v, want the held user message", mode, messageRoles(rest))
		}
	}
}

// While the predicate defers user input, a run ends without draining it; once
// the predicate clears, Continue delivers it in its own run.
func TestDeferredFollowUpSurvivesTheRun(t *testing.T) {
	response := createAssistantMessage(ai.ContentList{ai.TextContent{Text: "ok"}}, ai.StopStop)
	streamFn, calls := mockStreamFn(response, response)
	hold := true
	agent, err := NewAgent(&AgentOptions{
		InitialState: &AgentInitialState{Model: createModel()},
		StreamFn:     streamFn,
		ConvertToLlm: identityConverter,
		DeferFollowUp: func(message ai.Message) bool {
			return hold && ai.RoleOf(message) == ai.RoleUser
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	agent.FollowUp(createUserMessage("held follow-up"))
	if err := agent.PromptText(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Fatalf("model calls = %d, want the follow-up held", *calls)
	}
	if !agent.HasQueuedMessages() || agent.HasDeliverableMessages() {
		t.Fatalf("queued/deliverable = %v/%v, want the follow-up held", agent.HasQueuedMessages(), agent.HasDeliverableMessages())
	}

	hold = false
	if err := agent.Continue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if *calls != 2 {
		t.Fatalf("model calls = %d, want the released follow-up to run", *calls)
	}
	if agent.HasQueuedMessages() {
		t.Fatal("follow-up still queued after release")
	}
}

// Taking one message out of the queue must release its slot: the deferring
// drain is the hot path during a goal, and a stale tail slot would keep the
// delivered message alive until the next enqueue.
func TestDrainDeferringClearsConsumedSlots(t *testing.T) {
	for _, mode := range []QueueMode{QueueModeOneAtATime, QueueModeAll} {
		queue := &pendingMessageQueue{mode: mode}
		queue.enqueue(createUserMessage("first"))
		queue.enqueue(createUserMessage("second"))
		taken := queue.drainDeferring(func(ai.Message) bool { return false })
		wantTaken, wantLeft := 1, 1
		if mode == QueueModeAll {
			wantTaken, wantLeft = 2, 0
		}
		if len(taken) != wantTaken || len(queue.messages) != wantLeft {
			t.Fatalf("%s: took %d left %d, want %d/%d", mode, len(taken), len(queue.messages), wantTaken, wantLeft)
		}
		backing := queue.messages[:cap(queue.messages)]
		for index := len(queue.messages); index < len(backing); index++ {
			if backing[index] != nil {
				t.Fatalf("%s: slot %d retains a consumed message", mode, index)
			}
		}
	}
}
