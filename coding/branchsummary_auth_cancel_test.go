package coding

import (
	"context"
	"errors"
	"github.com/dat267/pier/ai"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), core/agent-session.ts navigateTree
// passes its branch-summary signal into _getSummarizationRequestAuth.
func TestBranchSummaryAbortCancelsAuthResolution(t *testing.T) {
	for _, abort := range []bool{false, true} {
		name := "control"
		if abort {
			name = "abort"
		}
		t.Run(name, func(t *testing.T) {
			var session *AgentSession
			observed := false
			cancelReachedAuth := false
			provider := stubProvider("alpha")
			provider.Auth.APIKey.Resolve = func(input ai.AuthResolveInput) (*ai.AuthResult, error) {
				if observed {
					if abort {
						session.AbortBranchSummary()
					}
					cancelReachedAuth = input.Ctx2.Err() != nil
					if err := input.Ctx2.Err(); err != nil {
						return nil, err
					}
				}
				return &ai.AuthResult{Auth: ai.ModelAuth{APIKey: "synthetic-key"}}, nil
			}
			runtime := runtimeWithProviders(t, provider)
			model := runtime.GetModel("alpha", "alpha-model")
			model.ContextWindow = 100000
			manager := sessionWithBranch(t)
			root := manager.GetEntries()[0].ID
			var err error
			session, err = NewAgentSession(&SessionConfig{Cwd: t.TempDir(), Sessions: manager, Model: model, StreamFn: stubStreamFn, Control: &AgentSessionControl{ModelRuntime: runtime}})
			if err != nil {
				t.Fatal(err)
			}
			observed = true
			_, err = session.NavigateTree(context.Background(), root, NavigateTreeOptions{Summarize: true})
			if abort {
				if !cancelReachedAuth {
					t.Fatal("AbortBranchSummary did not cancel auth resolution context")
				}
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled auth error = %v, want context.Canceled", err)
				}
				for _, entry := range manager.GetEntries() {
					if entry.Type == "branch_summary" {
						t.Fatal("cancelled auth persisted a summary")
					}
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
