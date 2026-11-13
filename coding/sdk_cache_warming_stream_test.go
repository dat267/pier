package coding

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// pi v1.5.2 (b4afdddf), core/sdk.ts:334 hands the model runtime to the cache
// warmer, so a refresh streams through the provider. A warmer without a stream
// function leaves every refresh an unreported no-op: the recovered nil call
// reschedules the timer and no warm request is ever bought.
func TestSDKCacheWarmingStreamsThroughTheModelRuntime(t *testing.T) {
	tempAgentDir(t)
	model := warmerModel()
	var warmCalls atomic.Int64
	var warmOptions atomic.Pointer[ai.SimpleStreamOptions]
	provider := ai.CreateProvider(ai.CreateProviderOptions{
		ID: "anthropic", Name: "anthropic",
		Auth: ai.ProviderAuth{APIKey: &ai.ApiKeyAuth{
			Name: "key",
			Resolve: func(ai.AuthResolveInput) (*ai.AuthResult, error) {
				return &ai.AuthResult{Auth: ai.ModelAuth{APIKey: "sk"}, Source: "test"}, nil
			},
		}},
		Models: []*ai.Model{model},
		Single: funcStreams{streamSimple: func(_ *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			warmCalls.Add(1)
			warmOptions.Store(options)
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
					API: ai.APIAnthropicMessages, Provider: "anthropic", Model: model.ID, StopReason: ai.StopStop,
					Usage: ai.Usage{Input: 10, CacheRead: 1000, TotalTokens: 1010},
				}})
			}()
			return stream
		}},
	})
	runtime := runtimeWithProviders(t, provider)
	sessions := NewSessionManager(t.TempDir(), nil)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	created, err := CreateAgentSession(context.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), AgentDir: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings, Model: model,
		SessionManager: sessions,
		NoSkills:       true, NoContextFiles: true, NoPromptTemplates: true,
		StreamFn: func(model *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
					API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopStop,
					Content: ai.ContentList{ai.TextContent{Text: "answer"}},
					Usage:   ai.Usage{Input: 100000, CacheRead: 0, Output: 10, TotalTokens: 100010},
				}})
			}()
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session := created.Session
	defer session.Dispose()
	// The economics decision has its own tests; the host override keeps this
	// one on the wiring: a refresh that the warmer buys must reach the provider.
	session.CacheWarmer.Decide = func(CacheWarmingDecisionEvent) CacheWarmingAction {
		return CacheWarmingActionWarm
	}
	if err := session.Prompt(context.Background(), "warm me", nil); err != nil {
		t.Fatal(err)
	}
	if status := session.GetCacheWarmingStatus(); status == nil || status.State != "scheduled" {
		t.Fatalf("warm run not scheduled: %+v", status)
	}
	// warmerModel()'s 11-second cache tier schedules the refresh one second out.
	deadline := time.Now().Add(5 * time.Second)
	for warmCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if calls := warmCalls.Load(); calls != 1 {
		t.Fatalf("warm requests = %d, want 1 through the model runtime", calls)
	}
	options := warmOptions.Load()
	if options == nil || options.MaxTokens == nil || *options.MaxTokens != 1 || !options.DisableContinuation {
		t.Fatalf("warm options = %+v, want a one-token replay that keeps the continuation state", options)
	}
	// The refresh bills the provider, so its spend has to reach the session as a
	// cache_warm usage entry (upstream cache-warmer.ts appendUsage). The stream
	// finishes on its own goroutine, so the entry is the condition to wait for:
	// reading the branch right after the request arrived lost it under -race and
	// failed this test intermittently.
	warmed := 0
	usageDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(usageDeadline) {
		warmed = 0
		for _, entry := range sessions.GetBranch("") {
			if entry.Type == "usage" && entry.Kind == "cache_warm" {
				warmed++
			}
		}
		if warmed > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if warmed != 1 {
		t.Fatalf("cache_warm usage entries = %d, want 1", warmed)
	}
}
