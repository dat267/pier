package coding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dat267/pier/ai"
)

// Port of the registration and routing half of
// pi v1.5.2 (b4afdddf) core/model-runtime.ts: registerVirtualModel,
// unregisterVirtualModel, resolveModel and getPhysicalModel. A virtual model
// belongs to a provider id but never reaches a provider: the selection names it,
// and every request is routed to a physical catalog model.

// VirtualModelResolveOptions are the inputs of one routing request
// (upstream's resolveModel options).
type VirtualModelResolveOptions struct {
	// Reason is why the request is being routed: "user", "continuation",
	// "retry" or "direct".
	Reason string
	// ThinkingLevel is the selected level; the router decides what it means.
	ThinkingLevel string
	// Failed is the failed response of a retry, which Messages no longer holds.
	Failed *ai.AssistantMessage
	// State is the router state this branch stored last, if any.
	State json.RawMessage
	// Signal cancels routing.
	Signal context.Context
}

type registeredVirtualModel struct {
	model *ai.Model
	route func(ModelRouteRequest) (ModelRoute, error)
}

// RegisterVirtualModel registers a virtual model under definition.Provider,
// replacing one with the same id, and adds it to that provider's catalog. It
// fails when the provider or id is blank, or when the id belongs to a physical
// model of that provider.
func (r *ModelRuntime) RegisterVirtualModel(definition VirtualModelDefinition) error {
	providerID, id := definition.Provider, definition.ID
	if strings.TrimSpace(providerID) == "" || strings.TrimSpace(id) == "" {
		return errors.New("Virtual model provider and id must not be empty.")
	}
	if existing := r.models.GetModel(providerID, id); existing != nil && !IsVirtualModel(existing) {
		return fmt.Errorf("Virtual model %s/%s conflicts with a physical model.", providerID, id)
	}
	if definition.Route == nil {
		return fmt.Errorf("Virtual model %s/%s has no router.", providerID, id)
	}
	registered := &registeredVirtualModel{model: CreateVirtualModel(definition), route: definition.Route}

	r.mu.Lock()
	if r.virtualModels == nil {
		r.virtualModels = map[string]map[string]*registeredVirtualModel{}
	}
	models := r.virtualModels[providerID]
	if models == nil {
		models = map[string]*registeredVirtualModel{}
		r.virtualModels[providerID] = models
	}
	models[id] = registered
	r.mu.Unlock()

	r.recomposeProvider(providerID)
	composed := r.models.GetProvider(providerID) != nil
	r.updateModelSnapshot()
	if !composed && !r.HasConfiguredAuth(providerID) {
		// A provider of only virtual models needs no credentials, so mark it
		// configured now: session restore checks auth before a refresh lands.
		r.mu.Lock()
		if r.snapshot.Auth == nil {
			r.snapshot.Auth = map[string]*ai.AuthCheck{}
		}
		r.snapshot.Auth[providerID] = &ai.AuthCheck{Source: "virtual", Type: ai.AuthTypeAPIKey}
		if r.snapshot.ConfiguredProviders == nil {
			r.snapshot.ConfiguredProviders = map[string]bool{}
		}
		r.snapshot.ConfiguredProviders[providerID] = true
		r.mu.Unlock()
	}
	r.queueAvailabilityRefresh(context.Background())
	return nil
}

// UnregisterVirtualModel removes a virtual model and its catalog entry.
func (r *ModelRuntime) UnregisterVirtualModel(providerID, modelID string) {
	r.mu.Lock()
	models := r.virtualModels[providerID]
	_, present := models[modelID]
	if present {
		delete(models, modelID)
		if len(models) == 0 {
			delete(r.virtualModels, providerID)
		}
	}
	r.mu.Unlock()
	if !present {
		return
	}
	r.recomposeProvider(providerID)
	r.updateModelSnapshot()
	r.queueAvailabilityRefresh(context.Background())
}

// virtualModelsFor returns a provider's registered virtual catalog entries.
func (r *ModelRuntime) virtualModelsFor(providerID string) []*ai.Model {
	r.mu.Lock()
	defer r.mu.Unlock()
	models := r.virtualModels[providerID]
	if len(models) == 0 {
		return nil
	}
	out := make([]*ai.Model, 0, len(models))
	for _, registered := range models {
		out = append(out, registered.model)
	}
	slices.SortFunc(out, func(left, right *ai.Model) int { return strings.Compare(left.ID, right.ID) })
	return out
}

// GetPhysicalModel returns a catalog chat model that is not virtual.
func (r *ModelRuntime) GetPhysicalModel(providerID, modelID string) *ai.Model {
	model := r.GetModel(providerID, modelID)
	if model == nil || IsVirtualModel(model) {
		return nil
	}
	return model
}

// ResolveVirtualModel asks a virtual model's router for the physical model and
// thinking level of one request. The router must name a physical catalog model
// whose provider has credentials, and the level is clamped to that model.
func (r *ModelRuntime) ResolveVirtualModel(model *ai.Model, messages []ai.Message, options VirtualModelResolveOptions) (ModelRoute, error) {
	name := fmt.Sprintf("Virtual model %s/%s", model.Provider, model.ID)
	r.mu.Lock()
	registered := r.virtualModels[model.Provider][model.ID]
	r.mu.Unlock()
	if registered == nil {
		return ModelRoute{}, fmt.Errorf("%s is not registered.", name)
	}

	request := ModelRouteRequest{
		Model:         model,
		ThinkingLevel: options.ThinkingLevel,
		Reason:        options.Reason,
		State:         options.State,
		Messages:      messages,
		Signal:        options.Signal,
	}
	if latest := FindLatestResponse(messages); latest != nil {
		if previous := r.GetPhysicalModel(latest.Provider, latest.Model); previous != nil {
			selection := ModelRouteSelection{Model: previous}
			if latest.ThinkingLevel != nil {
				level := *latest.ThinkingLevel
				selection.ThinkingLevel = &level
			}
			request.Previous = &selection
		}
	}
	if options.Failed != nil {
		if failedModel := r.GetPhysicalModel(options.Failed.Provider, options.Failed.Model); failedModel != nil {
			selection := ModelRouteSelection{Model: failedModel}
			if options.Failed.ThinkingLevel != nil {
				level := *options.Failed.ThinkingLevel
				selection.ThinkingLevel = &level
			}
			request.Failed = &ModelRouteFailure{Selection: selection, Message: options.Failed}
		}
	}

	route, err := registered.route(request)
	if err != nil {
		return ModelRoute{}, err
	}
	if route.Model == nil {
		return ModelRoute{}, fmt.Errorf("%s routed to no model.", name)
	}
	routed := fmt.Sprintf("%s routed to %s/%s", name, route.Model.Provider, route.Model.ID)
	target := r.GetPhysicalModel(route.Model.Provider, route.Model.ID)
	if target == nil {
		return ModelRoute{}, fmt.Errorf("%s, which is not a physical model.", routed)
	}
	if !r.HasConfiguredAuth(target.Provider) {
		return ModelRoute{}, fmt.Errorf("%s, which has no credentials.", routed)
	}
	route.Model = target
	route.ThinkingLevel = (ai.ClampThinkingLevel(target, route.ThinkingLevel))
	return route, nil
}
