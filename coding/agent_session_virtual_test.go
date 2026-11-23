package coding

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// pi v1.5.2 (b4afdddf), core/agent-session.ts prepareRequest and the
// pi.virtual-model-state entry: a virtual selection stays in agent state, each
// request is routed to a physical model for that request only, and new router
// state lands on the branch.
func TestSessionRoutesAVirtualSelection(t *testing.T) {
	tempAgentDir(t)
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		AuthPath:        filepath.Join(GetAgentDir(), "auth.json"),
		ModelsPath:      filepath.Join(GetAgentDir(), "models.json"),
		Credentials:     newMemoryCredentialStore(),
		RefreshOnCreate: new(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	physical := runtime.GetModels("anthropic")[0]
	if err := runtime.SetRuntimeAPIKey(ctx, physical.Provider, "synthetic-key"); err != nil {
		t.Fatal(err)
	}
	var reasons []string
	stateJSON := json.RawMessage(`{"turn":1}`)
	definition := VirtualModelDefinition{
		Provider: "anthropic", ID: "session-router", Name: "Session router",
		ThinkingLevels: []string{"off", "high"},
		Route: func(request ModelRouteRequest) (ModelRoute, error) {
			reasons = append(reasons, request.Reason)
			return ModelRoute{
				Model:         &ai.Model{Provider: physical.Provider, ID: physical.ID},
				ThinkingLevel: "high", State: stateJSON,
			}, nil
		},
	}
	if err := runtime.RegisterVirtualModel(definition); err != nil {
		t.Fatal(err)
	}
	virtual := runtime.GetModel("anthropic", "session-router")

	var streamedModels []string
	streamCalls := 0
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	created, err := CreateAgentSession(ctx, &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings, Model: virtual,
		StreamFn: func(model *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			streamedModels = append(streamedModels, model.ID)
			streamCalls++
			response := &ai.AssistantMessage{
				API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopStop,
				Content: ai.ContentList{ai.TextContent{Text: "routed answer"}}, Usage: ai.Usage{Input: 1, TotalTokens: 1},
			}
			if streamCalls == 1 {
				response = &ai.AssistantMessage{
					API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopToolUse,
					Content: ai.ContentList{ai.ToolCall{ID: "call", Name: "get_goal", Arguments: []byte(`{}`)}},
					Usage:   ai.Usage{Input: 1, TotalTokens: 1},
				}
			}
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: response.StopReason, Message: response})
			}()
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session := created.Session
	defer session.Dispose()

	if err := session.Prompt(ctx, "route this", nil); err != nil {
		t.Fatal(err)
	}
	if len(streamedModels) == 0 {
		t.Fatal("no request was streamed")
	}
	for index, id := range streamedModels {
		if id != physical.ID {
			t.Fatalf("request %d streamed %q, want the routed physical model", index, id)
		}
	}
	if len(reasons) == 0 || reasons[0] != "user" {
		t.Fatalf("router reasons = %v, want the first request marked user", reasons)
	}
	if selected := session.Model(); selected == nil || !IsVirtualModel(selected) {
		t.Fatalf("selection = %+v, want the virtual model kept in agent state", selected)
	}
	state, ok := GetVirtualModelState(session.Sessions.GetBranch(""), "anthropic", "session-router")
	if !ok || string(state) != string(stateJSON) {
		t.Fatalf("branch state = %s (found %v), want the router state", state, ok)
	}
}

// A retry records the response it replaces so the next routed request reports
// reason "retry". The failure belongs to that request, not to a later virtual
// selection: a request of a physical selection consumes it.
func TestVirtualRouteFailureDoesNotOutliveItsRequest(t *testing.T) {
	tempAgentDir(t)
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		AuthPath:        filepath.Join(GetAgentDir(), "auth.json"),
		ModelsPath:      filepath.Join(GetAgentDir(), "models.json"),
		Credentials:     newMemoryCredentialStore(),
		RefreshOnCreate: new(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	physical := runtime.GetModels("anthropic")[0]
	if err := runtime.SetRuntimeAPIKey(ctx, physical.Provider, "synthetic-key"); err != nil {
		t.Fatal(err)
	}
	var reasons []string
	if err := runtime.RegisterVirtualModel(VirtualModelDefinition{
		Provider: "anthropic", ID: "retry-router", Name: "Retry router",
		Route: func(request ModelRouteRequest) (ModelRoute, error) {
			reasons = append(reasons, request.Reason)
			return ModelRoute{Model: &ai.Model{Provider: physical.Provider, ID: physical.ID}, ThinkingLevel: "off"}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	virtual := runtime.GetModel("anthropic", "retry-router")
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	created, err := CreateAgentSession(ctx, &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings, Model: virtual,
		StreamFn: func(*ai.Model, ai.TranscriptContext, *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			return ai.NewAssistantMessageEventStream()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session := created.Session
	defer session.Dispose()

	messages := []ai.Message{&ai.UserMessage{Content: ai.StringOrBlocks{Text: "go"}}}
	virtualRequest := &agent.PrepareRequestContext{Model: virtual, Context: agent.AgentContext{Messages: messages}}
	// The request right after a retry is the one that reports it.
	session.setRouteFailure(&ai.AssistantMessage{Provider: physical.Provider, Model: physical.ID, StopReason: ai.StopError})
	if update, err := session.prepareVirtualRequest(ctx, virtualRequest); err != nil || update == nil {
		t.Fatalf("retry request = %+v, %v; want routing", update, err)
	}
	if len(reasons) != 1 || reasons[0] != "retry" {
		t.Fatalf("router reasons = %v, want the retry marked", reasons)
	}
	session.setRouteFailure(&ai.AssistantMessage{Provider: physical.Provider, Model: physical.ID, StopReason: ai.StopError})
	physicalRequest := &agent.PrepareRequestContext{Model: physical, Context: agent.AgentContext{Messages: messages}}
	if update, err := session.prepareVirtualRequest(ctx, physicalRequest); err != nil || update != nil {
		t.Fatalf("physical request = %+v, %v; want no routing", update, err)
	}
	if update, err := session.prepareVirtualRequest(ctx, virtualRequest); err != nil || update == nil {
		t.Fatalf("virtual request = %+v, %v; want routing", update, err)
	}
	if len(reasons) != 2 || reasons[1] != "user" {
		t.Fatalf("router reasons = %v, want the consumed failure to leave a user request", reasons)
	}
}
