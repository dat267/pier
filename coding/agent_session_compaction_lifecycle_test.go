package coding

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.3.0 packages/coding-agent/src/core/agent-session.ts:
// _runAutoCompaction installs its controller before compaction_start;
// abort cancels both compaction controllers.
func TestCompactionStartCanBeAborted(t *testing.T) {
	for _, manual := range []bool{false, true} {
		name := "automatic"
		if manual {
			name = "manual"
		}
		t.Run(name, func(t *testing.T) {
			var runs atomic.Int64
			s := compactionTestSession(t, &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}, summarizingStreamFn(t, &runs, "summary text long enough to pass the usability check"), smallCompactionSettings())
			seedBranch(t, s)
			var end *SessionEvent
			s.Subscribe(func(e *SessionEvent) {
				if e.Type == SessionCompactionStart {
					if manual {
						s.AbortAsync()
					} else {
						s.AbortCompaction()
					}
				}
				if e.Type == SessionCompactionEnd {
					end = e
				}
			})
			var err error
			if manual {
				_, err = s.CompactSession(context.Background(), "")
			} else {
				_, err = s.RunAutoCompaction(context.Background(), CompactionThreshold, false)
			}
			if err == nil {
				t.Fatal("cancelled compaction completed successfully")
			}
			if end == nil || !end.Aborted || end.Result != nil {
				t.Fatalf("end = %+v", end)
			}
			for _, e := range s.Sessions.GetEntries() {
				if e.Type == "compaction" {
					t.Fatal("cancelled compaction was persisted")
				}
			}
			if !s.IsIdle() {
				t.Fatal("cancelled compaction did not become idle")
			}
			// The reference checks the signal right after compaction_start, before
			// any summarization request.
			if !manual && runs.Load() != 0 {
				t.Fatalf("cancelled automatic compaction made %d summarization calls", runs.Load())
			}
		})
	}
}

// Reference: pi v1.3.0 core/agent-session.ts abort -> abortBranchSummary.
func TestAbortAsyncCancelsBranchSummary(t *testing.T) {
	s := compactionTestSession(t, nil, unreachableStreamFn, smallCompactionSettings())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.control.stateMu.Lock()
	s.control.branchSummaryCancel = cancel
	s.control.stateMu.Unlock()
	s.AbortAsync()
	if ctx.Err() == nil {
		t.Fatal("branch summary was not cancelled")
	}
}

// Reference: pi v1.3.0 core/agent-session.ts _clearManualCompactionState and
// _runAutoCompaction finally release idle waiters after clearing controllers.
func TestCompactionCompletionReleasesIdleWaiters(t *testing.T) {
	for _, manual := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			name := "automatic"
			if manual {
				name = "manual"
			}
			if fail {
				name += "/failure"
			} else {
				name += "/success"
			}
			t.Run(name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					var runs atomic.Int64
					release := make(chan struct{})
					stream := summarizingStreamFn(t, &runs, "summary text long enough to pass the usability check")
					if fail {
						stream = erroringStreamFn(100, "nonretryable failure")
					}
					s := compactionTestSession(t, &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}, func(m *ai.Model, c ai.TranscriptContext, o *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
						<-release
						return stream(m, c, o)
					}, smallCompactionSettings())
					seedBranch(t, s)
					done := make(chan error, 1)
					go func() {
						if manual {
							_, err := s.CompactSession(context.Background(), "")
							done <- err
						} else {
							_, err := s.RunAutoCompaction(context.Background(), CompactionThreshold, false)
							done <- err
						}
					}()
					synctest.Wait()
					if !s.IsCompacting() {
						t.Fatal("not compacting")
					}
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					idle := make(chan error, 1)
					go func() { idle <- s.WaitForIdle(ctx) }()
					synctest.Wait()
					close(release)
					if err := <-done; (err != nil) != fail {
						t.Fatalf("compaction error = %v, failure = %v", err, fail)
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
						t.Fatal("WaitForIdle remains blocked after compaction finishes")
					}
				})
			})
		}
	}
}
