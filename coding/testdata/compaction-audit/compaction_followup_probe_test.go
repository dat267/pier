package coding

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.3.0 core/compaction/utils.ts computeFileLists uses
// JavaScript's default UTF-16 code-unit sort, not UTF-8 byte order.
func TestAuditCompactionFileListsUseUTF16Order(t *testing.T) {
	ops := CreateFileOps()
	ExtractFileOpsFromMessage(&ai.AssistantMessage{Content: ai.ContentList{
		ai.ToolCall{ID: "a", Name: "read", Arguments: json.RawMessage(`{"path":"\ue000.txt"}`)},
		ai.ToolCall{ID: "b", Name: "read", Arguments: json.RawMessage(`{"path":"😀.txt"}`)},
	}}, ops)
	read, _ := ComputeFileLists(ops)
	if got := strings.Join(read, ","); got != "😀.txt,\ue000.txt" {
		t.Fatalf("read-file order = %q, want UTF-16 order %q", got, "😀.txt,\ue000.txt")
	}
}

// Reference: pi v1.3.0 core/agent-session.ts _getSummarizationRequestAuth
// passes the compaction signal into auth resolution in both compaction paths.
func TestAuditCompactionAuthReceivesRequestContext(t *testing.T) {
	for _, manual := range []bool{true, false} {
		name := "automatic"
		if manual {
			name = "manual"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			observe := false
			cancelReachedAuth := false
			provider := stubProvider("alpha")
			provider.Auth.APIKey.Resolve = func(input ai.AuthResolveInput) (*ai.AuthResult, error) {
				if observe {
					cancel()
					cancelReachedAuth = input.Ctx2.Err() != nil
				}
				return &ai.AuthResult{Auth: ai.ModelAuth{APIKey: "test-key"}}, nil
			}
			runtime := runtimeWithProviders(t, provider)
			model := runtime.GetModel("alpha", "alpha-model")
			var runs atomic.Int64
			session := compactionTestSession(t, model, summarizingStreamFn(t, &runs, "summary"), smallCompactionSettings())
			session.control.ModelRuntime = runtime
			seedBranch(t, session)
			observe = true
			var err error
			if manual {
				_, err = session.CompactSession(ctx, "")
			} else {
				_, err = session.RunAutoCompaction(ctx, CompactionThreshold, false)
			}
			if err == nil {
				t.Fatal("cancelled compaction did not report cancellation")
			}
			if !cancelReachedAuth {
				t.Fatal("cancelling compaction did not cancel its auth resolution context")
			}
		})
	}
}

// Reference: pi v1.3.0 core/agent-session.ts _omitRecoveryAttempt refreshes
// finalized context immediately, even if the subsequent compaction fails.
func TestAuditCompactionFailureKeepsLiveContextProjected(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
	base := erroringStreamFn(1, "prompt is too long: 2000 tokens > 1000 maximum")
	calls := 0
	replayedOmission := false
	session := compactionTestSession(t, model, func(m *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		calls++
		if calls == 2 {
			for _, message := range transcript.Messages {
				if assistant, ok := message.(*ai.AssistantMessage); ok && assistant.StopReason == ai.StopError {
					replayedOmission = true
				}
			}
		}
		return base(m, transcript, options)
	}, smallCompactionSettings())
	seedBranch(t, session)
	session.CompactionStreamFn = erroringStreamFn(1, "summarizer unavailable")
	err := session.PromptText(context.Background(), "continue the task")
	if err == nil || !strings.Contains(err.Error(), "summarizer unavailable") {
		t.Fatalf("expected the summary failure, got %v", err)
	}
	for _, message := range session.Sessions.Projection().Messages {
		if assistant, ok := message.(*ai.AssistantMessage); ok && assistant.StopReason == ai.StopError {
			t.Fatal("reproduction did not persist the overflow omission")
		}
	}
	if err := session.PromptText(context.Background(), "continue after compaction failure"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("assistant requests = %d, want 2", calls)
	}
	if replayedOmission {
		t.Fatal("the next provider request replayed the persistently omitted overflow response")
	}
}
