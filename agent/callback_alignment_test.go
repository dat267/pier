package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/dat267/pier/ai"
)

type runContextKey struct{}

// Upstream agent.ts passes the active abort signal to prepareRequest and finishTurn.
func TestAgentRequestHooksReceiveActiveRunContext(t *testing.T) {
	want := "run-value"
	runCtx := context.WithValue(context.Background(), runContextKey{}, want)
	var prepareSeen, finishSeen bool
	a, err := NewAgent(&AgentOptions{
		StreamFn: unusedStreamFn(),
		PrepareRequest: func(request *PrepareRequestContext, ctx context.Context) (*AgentRequestUpdate, error) {
			prepareSeen = ctx.Value(runContextKey{}) == want
			if len(request.Context.Messages) == 0 || ai.RoleOf(request.Context.Messages[len(request.Context.Messages)-1]) != ai.RoleUser {
				t.Fatalf("request context messages = %v", messageRoles(request.Context.Messages))
			}
			return nil, nil
		},
		FinishTurn: func(turn *AgentTurnContext, ctx context.Context) (*AgentTurnDecision, error) {
			finishSeen = ctx.Value(runContextKey{}) == want && turn.Message != nil
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.PromptText(runCtx, "hello"); err != nil {
		t.Fatal(err)
	}
	if !prepareSeen || !finishSeen {
		t.Fatalf("hooks saw active context: prepare=%v finish=%v", prepareSeen, finishSeen)
	}
}

// Upstream agent.ts folds rejected loop callbacks into its run-failure lifecycle.
func TestFinishTurnErrorFailsRun(t *testing.T) {
	wantErr := errors.New("finish failed")
	a, err := NewAgent(&AgentOptions{
		StreamFn: unusedStreamFn(),
		FinishTurn: func(*AgentTurnContext, context.Context) (*AgentTurnDecision, error) {
			return nil, wantErr
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.PromptText(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if got := a.State().ErrorMessage; got != wantErr.Error() {
		t.Fatalf("ErrorMessage = %q, want %q", got, wantErr)
	}
}

// Upstream agent-loop.ts awaits getApiKey; rejection fails the run instead of falling back silently.
func TestGetAPIKeyErrorFailsRun(t *testing.T) {
	wantErr := errors.New("token refresh failed")
	streamCalled := false
	a, err := NewAgent(&AgentOptions{
		StreamFn: func(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			streamCalled = true
			return nil
		},
		GetAPIKey: func(string, context.Context) (string, error) {
			return "", wantErr
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.PromptText(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if streamCalled {
		t.Fatal("provider stream called after API-key callback failed")
	}
	if got := a.State().ErrorMessage; got != wantErr.Error() {
		t.Fatalf("ErrorMessage = %q, want %q", got, wantErr)
	}
}

// Upstream agent.ts createLoopConfig copies prepareRequest and finishTurn into
// the loop config, so a hook assigned during a run takes effect on the next run,
// not the current one.
func TestRunKeepsTheRequestHooksItStartedWith(t *testing.T) {
	var calls []string
	var a *Agent
	replacement := func(*AgentTurnContext, context.Context) (*AgentTurnDecision, error) {
		calls = append(calls, "replacement")
		return nil, nil
	}
	a, err := NewAgent(&AgentOptions{
		StreamFn: unusedStreamFn(),
		PrepareRequest: func(*PrepareRequestContext, context.Context) (*AgentRequestUpdate, error) {
			a.FinishTurn = replacement
			return nil, nil
		},
		FinishTurn: func(*AgentTurnContext, context.Context) (*AgentTurnDecision, error) {
			calls = append(calls, "original")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.PromptText(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if err := a.PromptText(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != "original" || calls[1] != "replacement" {
		t.Fatalf("finishTurn calls = %v, want [original replacement]", calls)
	}
}
