package coding

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.3.0 core/agent-session.ts navigateTree passes the session's
// retry settings to generateBranchSummary, just as compaction does.
func TestNavigateTreeUsesSummarizationRetrySettings(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := sessionWithBranch(t)
		session, err := NewAgentSession(&SessionConfig{Cwd: t.TempDir(), Sessions: manager,
			Model:    &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192},
			StreamFn: erroringStreamFn(1, "server overloaded"), Settings: SessionSettings{Retry: &ai.RetryPolicy{Enabled: true, MaxRetries: 1, BaseDelayMS: 1}},
		})
		if err != nil {
			t.Fatal(err)
		}
		var scheduled bool
		session.Subscribe(func(e *SessionEvent) {
			if e.Type == SessionSummarizationRetryScheduled {
				scheduled = true
			}
		})
		result, err := session.NavigateTree(context.Background(), manager.GetEntries()[0].ID, NavigateTreeOptions{Summarize: true})
		if err != nil {
			t.Fatal(err)
		}
		if result.SummaryEntry == nil || !scheduled {
			t.Fatalf("summary=%+v retry scheduled=%v", result.SummaryEntry, scheduled)
		}
	})
}

// Reference: pi v1.3.0 core/agent-session.ts navigateTree's finally clears
// _branchSummaryAbortController and calls _resolveIdleWaitIfIdle.
func TestNavigateTreeReleasesIdleWaiters(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "success"
		if failure {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				manager := sessionWithBranch(t)
				release := make(chan struct{})
				failures := int64(0)
				if failure {
					failures = 1
				}
				stream := erroringStreamFn(failures, "nonretryable failure")
				session, err := NewAgentSession(&SessionConfig{Cwd: t.TempDir(), Sessions: manager,
					Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192},
					StreamFn: func(m *ai.Model, c ai.TranscriptContext, o *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
						<-release
						return stream(m, c, o)
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() {
					_, err := session.NavigateTree(context.Background(), manager.GetEntries()[0].ID, NavigateTreeOptions{Summarize: true})
					done <- err
				}()
				synctest.Wait()
				if !session.IsCompacting() {
					t.Fatal("navigation was not admitted")
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				idle := make(chan error, 1)
				go func() { idle <- session.WaitForIdle(ctx) }()
				synctest.Wait()
				close(release)
				if err := <-done; (err != nil) != failure {
					t.Fatalf("navigation error=%v failure=%v", err, failure)
				}
				synctest.Wait()
				select {
				case err := <-idle:
					if err != nil {
						t.Fatal(err)
					}
				default:
					cancel()
					<-idle
					t.Fatal("idle waiter outlived navigation")
				}
			})
		})
	}
}
