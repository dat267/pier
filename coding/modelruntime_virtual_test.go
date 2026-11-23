package coding

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.5.2 (b4afdddf), core/model-runtime.ts registerVirtualModel and
// resolveModel: a virtual model belongs to a provider, must not collide with a
// physical model, and routes each request to a physical catalog model that has
// credentials, clamping the thinking level to it.
func TestVirtualModelRegistrationAndRouting(t *testing.T) {
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
	target := &ai.Model{Provider: physical.Provider, ID: physical.ID}

	var requests []ModelRouteRequest
	definition := VirtualModelDefinition{
		Provider: "anthropic", ID: "router", Name: "Router",
		ThinkingLevels: []string{"off", "high"},
		Route: func(request ModelRouteRequest) (ModelRoute, error) {
			requests = append(requests, request)
			return ModelRoute{Model: target, ThinkingLevel: "max", State: json.RawMessage(`{"turn":1}`)}, nil
		},
	}
	if err := runtime.RegisterVirtualModel(definition); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RegisterVirtualModel(VirtualModelDefinition{Provider: "anthropic", ID: physical.ID, Name: "collision", Route: definition.Route}); err == nil ||
		!strings.Contains(err.Error(), "conflicts with a physical model") {
		t.Fatalf("collision error = %v", err)
	}
	if err := runtime.RegisterVirtualModel(VirtualModelDefinition{Provider: " ", ID: "x", Route: definition.Route}); err == nil {
		t.Fatal("a blank provider must be rejected")
	}
	virtual := runtime.GetModel("anthropic", "router")
	if virtual == nil || !IsVirtualModel(virtual) {
		t.Fatalf("catalog entry = %+v, want the virtual model", virtual)
	}

	messages := []ai.Message{
		&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}},
		&ai.AssistantMessage{Provider: physical.Provider, Model: physical.ID, StopReason: ai.StopStop},
	}
	// Without credentials the router's target is rejected.
	if _, err := runtime.ResolveVirtualModel(virtual, messages, VirtualModelResolveOptions{Reason: "user"}); err == nil ||
		!strings.Contains(err.Error(), "which has no credentials") {
		t.Fatalf("routing without credentials = %v", err)
	}
	if err := runtime.SetRuntimeAPIKey(ctx, physical.Provider, "synthetic-key"); err != nil {
		t.Fatal(err)
	}
	// The router runs before the target is checked, so the rejected attempt is
	// also on record.
	before := len(requests)
	route, err := runtime.ResolveVirtualModel(virtual, messages, VirtualModelResolveOptions{Reason: "user", State: json.RawMessage(`{"turn":0}`)})
	if err != nil {
		t.Fatal(err)
	}
	if route.Model == nil || route.Model.ID != physical.ID || IsVirtualModel(route.Model) {
		t.Fatalf("route model = %+v, want the physical catalog model", route.Model)
	}
	if route.ThinkingLevel != string(ai.ClampThinkingLevel(route.Model, ai.ThinkMax)) {
		t.Fatalf("level = %q, want the clamped level", route.ThinkingLevel)
	}
	if len(route.State) == 0 {
		t.Fatal("router state was dropped")
	}
	if len(requests) != before+1 {
		t.Fatalf("router calls = %d, want %d", len(requests), before+1)
	}
	routed := requests[before]
	if routed.Previous == nil || routed.Previous.Model.ID != physical.ID || routed.Previous.ThinkingLevel != nil {
		t.Fatalf("router previous = %+v, want the physical response", routed.Previous)
	}
	if string(routed.State) != `{"turn":0}` || routed.Reason != "user" {
		t.Fatalf("router request = %+v, want the branch state and reason", routed)
	}

	// A router that names a virtual or unknown model fails with the routed name.
	if err := runtime.RegisterVirtualModel(VirtualModelDefinition{
		Provider: "anthropic", ID: "bad", Name: "Bad",
		Route: func(ModelRouteRequest) (ModelRoute, error) {
			return ModelRoute{Model: virtual, ThinkingLevel: "off"}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ResolveVirtualModel(runtime.GetModel("anthropic", "bad"), messages, VirtualModelResolveOptions{}); err == nil ||
		!strings.Contains(err.Error(), "is not a physical model") {
		t.Fatalf("virtual target = %v", err)
	}
	// The failed response of a retry reaches the router.
	failed := &ai.AssistantMessage{Provider: physical.Provider, Model: physical.ID, StopReason: ai.StopError}
	if _, err := runtime.ResolveVirtualModel(virtual, messages, VirtualModelResolveOptions{Reason: "retry", Failed: failed}); err != nil {
		t.Fatal(err)
	}
	last := requests[len(requests)-1]
	if last.Reason != "retry" || last.Failed == nil || last.Failed.Message != failed {
		t.Fatalf("retry request = %+v", last)
	}
}

// A registered virtual model is listed in the provider's catalog after routing
// is removed again.
func TestVirtualModelUnregister(t *testing.T) {
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
	definition := VirtualModelDefinition{
		Provider: "router-only", ID: "only", Name: "Only",
		Route: func(ModelRouteRequest) (ModelRoute, error) { return ModelRoute{}, nil },
	}
	if err := runtime.RegisterVirtualModel(definition); err != nil {
		t.Fatal(err)
	}
	if model := runtime.GetModel("router-only", "only"); model == nil || !IsVirtualModel(model) {
		t.Fatalf("virtual-only provider entry = %+v", model)
	}
	if !runtime.HasConfiguredAuth("router-only") {
		t.Fatal("a virtual-only provider must count as configured")
	}
	runtime.UnregisterVirtualModel("router-only", "only")
	if model := runtime.GetModel("router-only", "only"); model != nil {
		t.Fatalf("unregistered model still listed: %+v", model)
	}
	if _, err := runtime.ResolveVirtualModel(&ai.Model{Provider: "router-only", ID: "only", API: VirtualModelAPI}, nil, VirtualModelResolveOptions{}); err == nil ||
		!strings.Contains(err.Error(), "is not registered") {
		t.Fatalf("resolve after unregister = %v", err)
	}
}
