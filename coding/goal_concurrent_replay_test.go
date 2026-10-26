package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

// D223/D143: default parallel tool execution may call set_goal concurrently.
// Each accepted transition must be persisted in machine order so replay can
// validate the same state. JavaScript's dispatch/apply executes synchronously.
func TestConcurrentGoalToolsPreserveReplayableTransitions(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		name := "sequential-control"
		if parallel {
			name = "parallel"
		}
		t.Run(name, func(t *testing.T) {
			manager, dir := newTestSession(t)
			session, err := NewAgentSession(&SessionConfig{Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, Goal: true})
			if err != nil {
				t.Fatal(err)
			}
			tool := session.GetToolDefinition("set_goal")
			if tool == nil {
				t.Fatal("set_goal not registered")
			}
			const workers = 16
			failures := make(chan error, workers)
			execute := func(index int) {
				params := json.RawMessage(fmt.Sprintf(`{"objective":"objective %d"}`, index))
				result, err := tool.Execute("call", params, context.Background(), nil)
				if err != nil {
					failures <- err
				} else if result.IsError {
					failures <- fmt.Errorf("set_goal failed: %+v", result.Content)
				}
			}
			if parallel {
				ready := make(chan struct{})
				var wait sync.WaitGroup
				for index := range workers {
					wait.Go(func() { <-ready; execute(index) })
				}
				close(ready)
				wait.Wait()
			} else {
				for index := range workers {
					execute(index)
				}
			}
			close(failures)
			for err := range failures {
				t.Error(err)
			}
			before := session.Goal.Snapshot().Goal
			if before == nil {
				t.Fatal("no goal after successful writes")
			}
			var warnings []string
			session.Goal.SetUIHooks(func(message, level string) {
				if level == "warning" {
					warnings = append(warnings, message)
				}
			})
			session.Goal.Start("startup")
			after := session.Goal.Snapshot().Goal
			if after == nil || after.ID != before.ID {
				t.Fatalf("goal could not replay accepted transitions: before=%+v after=%+v warnings=%v", before, after, warnings)
			}
		})
	}
}

// Durable append serialization must not extend across the public notification
// callback: hosts may read goal state from that callback.
func TestGoalTransitionNotificationCanReadState(t *testing.T) {
	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, Goal: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.GetToolDefinition("set_goal").Execute("create", json.RawMessage(`{"objective":"work"}`), context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	observed := false
	session.Goal.SetUIHooks(func(string, string) {
		observed = session.Goal.Snapshot().Goal != nil
	})
	done := make(chan struct{})
	go func() {
		session.Goal.Start("reload")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("goal notification could not reenter Snapshot")
	}
	if !observed {
		t.Fatal("reload notification did not observe the paused goal")
	}
}
