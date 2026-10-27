package coding

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/sdk.ts cacheContextIsCurrent preserves message
// identity for the warmed prefix. Replacing one message at the same length
// invalidates the warm run; appending while preserving the prefix does not.
func TestSDKCacheWarmerRejectsReplacedMessagePrefix(t *testing.T) {
	for _, replace := range []bool{false, true} {
		name := "append-control"
		if replace {
			name = "replace"
		}
		t.Run(name, func(t *testing.T) {
			tempAgentDir(t)
			runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
				AuthPath: filepath.Join(GetAgentDir(), "auth.json"), ModelsPath: filepath.Join(GetAgentDir(), "models.json"),
				Credentials: newMemoryCredentialStore(), RefreshOnCreate: new(false),
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.SetRuntimeAPIKey("anthropic", "synthetic-key", context.Background()); err != nil {
				t.Fatal(err)
			}
			settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
			settings.SetCacheWarmingMode(CacheWarmingIdle)
			model := warmerModel()
			created, err := CreateAgentSession(context.Background(), &CreateAgentSessionOptions{
				Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings, Model: model,
				StreamFn: func(model *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
					stream := ai.NewAssistantMessageEventStream()
					go func() {
						stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
							API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopStop,
							Content: ai.ContentList{ai.TextContent{Text: "answer"}}, Usage: ai.Usage{Input: 1000, TotalTokens: 1000},
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
			if err := session.Prompt(context.Background(), "original", nil); err != nil {
				t.Fatal(err)
			}
			if status := session.GetCacheWarmingStatus(); status == nil || status.State != "scheduled" {
				t.Fatalf("warm run not scheduled: %+v", status)
			}
			messages := append([]ai.Message(nil), session.Messages()...)
			if replace {
				found := false
				for index, message := range messages {
					if _, ok := message.(*ai.UserMessage); ok {
						messages[index] = &ai.UserMessage{Content: ai.StringOrBlocks{Text: "replaced"}, Timestamp: 2}
						found = true
						break
					}
				}
				if !found {
					t.Fatal("request user message missing")
				}
			} else {
				messages = append(messages, &ai.UserMessage{Content: ai.StringOrBlocks{Text: "appended"}, Timestamp: 3})
			}
			session.Agent.SetMessages(messages)
			status := session.GetCacheWarmingStatus()
			if replace {
				if status.State != "inactive" || status.Reason != "conversation context changed" {
					t.Fatalf("replaced prefix warmer = %+v, want inactive/context changed", status)
				}
			} else if status.State != "scheduled" {
				t.Fatalf("unchanged prefix stopped warming: %+v", status)
			}
		})
	}
}
