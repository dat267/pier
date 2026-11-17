package coding

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// A model with no context window cannot size a compaction threshold. Upstream
// compares `contextTokens > contextWindow - reserveTokens`; with an undefined
// window that is NaN and therefore false, so no compaction runs. The port's
// zero window made the threshold negative and compacted at every boundary.
func TestCheckCompactionSkipsAnUnknownContextWindow(t *testing.T) {
	ctx := context.Background()
	provider := ai.CreateProvider(ai.CreateProviderOptions{
		ID: "windowless", Name: "Windowless", BaseURL: "https://windowless.example.com",
		Auth: ai.ProviderAuth{APIKey: &ai.ApiKeyAuth{Name: "key", Resolve: func(ai.AuthResolveInput) (*ai.AuthResult, error) {
			return &ai.AuthResult{Auth: ai.ModelAuth{APIKey: "synthetic-key"}, Source: "stored credential"}, nil
		}}},
		Models: []*ai.Model{{
			ID: "no-window", Name: "No window", API: ai.APIOpenAICompletions, Provider: "windowless",
			BaseURL: "https://windowless.example.com", Input: []string{"text"},
			// Deliberately no ContextWindow.
			MaxTokens: 100,
		}},
		Single: funcStreams{},
	})
	runtime := runtimeWithProviders(t, provider)
	agentDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(agentDir, "settings.json"),
		[]byte(`{"compaction":{"reserveTokens":100,"keepRecentTokens":50}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := NewSettingsManagerFromFiles(t.TempDir(), agentDir, SettingsManagerCreateOptions{})
	var streamCalls int
	created, err := CreateAgentSession(ctx, &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
		Model: runtime.GetModels("windowless")[0],
		StreamFn: func(model *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
					API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopStop,
					Content: ai.ContentList{ai.TextContent{Text: "summary text long enough to pass the usability check"}},
					Usage:   ai.Usage{Input: 10, Output: 5, TotalTokens: 15},
				}})
			}()
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session := created.Session
	t.Cleanup(session.Dispose)
	for index := 0; index < 3; index++ {
		session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: strings.Repeat("context ", 300)}, Timestamp: int64(index*2 + 1)})
		session.Sessions.AppendMessage(&ai.AssistantMessage{
			API: ai.APIOpenAICompletions, Provider: "ghost", Model: "removed-model", StopReason: ai.StopStop,
			Content: ai.ContentList{ai.TextContent{Text: strings.Repeat("answer ", 300)}}, Timestamp: int64(index*2 + 2),
		})
	}
	// An assistant whose model the runtime does not know: the window falls back
	// to the selection's, which is zero.
	ghost := &ai.AssistantMessage{
		API: ai.APIOpenAICompletions, Provider: "ghost", Model: "removed-model", StopReason: ai.StopStop,
		Content: ai.ContentList{ai.TextContent{Text: "answer"}}, Timestamp: 99,
	}
	if _, err := session.CheckCompaction(ctx, ghost, true); err != nil {
		t.Fatal(err)
	}
	if streamCalls != 0 {
		t.Fatalf("a summarization stream ran %d times for an unknown context window", streamCalls)
	}
	if latest := session.Sessions.LatestCompaction(); latest != nil {
		t.Fatalf("compacted with an unknown window: %+v", latest)
	}
}
