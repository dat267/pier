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

// pi v1.5.0 6fed3cfb9 (#11556): summaries sent through the SDK stream function
// must not carry pre-resolved credentials, because the runtime would take them
// as an explicit API key instead of the stored OAuth login. A custom compaction
// stream function still receives them.
func TestSummariesSkipPreResolvedAuthForTheRuntimeStream(t *testing.T) {
	run := func(resolvesAuth bool, customCompaction bool) (int, string) {
		t.Helper()
		provider := stubProvider("alpha")
		resolveCalls := 0
		provider.Auth.APIKey.Resolve = func(ai.AuthResolveInput) (*ai.AuthResult, error) {
			resolveCalls++
			return &ai.AuthResult{Auth: ai.ModelAuth{APIKey: "synthetic-key"}}, nil
		}
		runtime := runtimeWithProviders(t, provider)
		model := runtime.GetModel("alpha", "alpha-model")
		model.ContextWindow = 100000
		manager := sessionWithBranch(t)
		root := manager.GetEntries()[0].ID
		seenKey := "unset"
		capture := func(called *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			seenKey = options.APIKey
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
					API: called.API, Provider: called.Provider, Model: called.ID,
					Content:    ai.ContentList{ai.TextContent{Text: "Summary of the branch"}},
					StopReason: ai.StopStop,
				}})
			}()
			return stream
		}
		session, err := NewAgentSession(&SessionConfig{Cwd: t.TempDir(), Sessions: manager, Model: model,
			StreamFn: stubStreamFn, StreamFnResolvesAuth: resolvesAuth,
			Control: &AgentSessionControl{ModelRuntime: runtime}})
		if err != nil {
			t.Fatal(err)
		}
		if customCompaction {
			// A caller-supplied compaction stream function still gets credentials.
			session.CompactionStreamFn = capture
		} else {
			// The marked runtime stream function receives the request without them.
			session.streamFn = capture
		}
		if _, err := session.NavigateTree(context.Background(), root, NavigateTreeOptions{Summarize: true}); err != nil {
			t.Fatal(err)
		}
		return resolveCalls, seenKey
	}

	calls, key := run(true, false)
	if calls != 0 {
		t.Fatalf("runtime-resolving stream pre-resolved auth %d time(s)", calls)
	}
	if key != "" {
		t.Fatalf("summary request carried apiKey %q", key)
	}
	calls, key = run(false, true)
	if calls == 0 {
		t.Fatal("a custom compaction stream function must receive resolved auth")
	}
	if key != "synthetic-key" {
		t.Fatalf("custom stream apiKey = %q", key)
	}
}
