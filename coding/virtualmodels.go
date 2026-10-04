package coding

import (
	"context"
	"encoding/json"

	"github.com/dat267/pier/ai"
)

// Port of core/virtual-models.ts: catalog entries that route each request to a
// physical model. The provider-wrapper half (`withVirtualModels`) is deferred
// (D189): the Go ai.Provider keeps its model accessor unexported.

// VirtualModelAPI is the API id of virtual catalog entries; a request for it
// fails unless routed first.
const VirtualModelAPI = "pi-virtual"

// VirtualModelStateEntry is the custom entry type that stores router state on
// the session branch.
const VirtualModelStateEntry = "pi.virtual-model-state"

// VirtualModelStateData is the data of a pi.virtual-model-state entry.
type VirtualModelStateData struct {
	Provider string          `json:"provider"`
	ModelID  string          `json:"modelId"`
	State    json.RawMessage `json:"state,omitempty"`
}

// Model route reasons.
const (
	ModelRouteUser         = "user"
	ModelRouteContinuation = "continuation"
	ModelRouteRetry        = "retry"
	ModelRouteDirect       = "direct"
)

// ModelRouteSelection is a physical model and thinking level, or a branch's
// recorded provider/model id.
type ModelRouteSelection struct {
	Model         *ai.Model `json:"model,omitempty"`
	ThinkingLevel *string   `json:"thinkingLevel,omitempty"`
	Provider      string    `json:"provider,omitempty"`
	ModelID       string    `json:"modelId,omitempty"`
}

// ModelRouteFailure is a router failure for a retry request.
type ModelRouteFailure struct {
	Selection ModelRouteSelection  `json:"selection"`
	Message   *ai.AssistantMessage `json:"message"`
}

// ModelRouteRequest is one routing request.
type ModelRouteRequest struct {
	Model         *ai.Model
	ThinkingLevel string
	Reason        string
	// Previous is the physical model and thinking level of the latest
	// successful response in Messages.
	Previous *ModelRouteSelection
	// Failed is the failed request for a retry.
	Failed *ModelRouteFailure
	// State is the router state last returned on this branch.
	State    json.RawMessage
	Messages []ai.Message
	Signal   context.Context
}

// ModelRoute is the physical model and thinking level for one request.
type ModelRoute struct {
	Model         *ai.Model
	ThinkingLevel string
	// State is new router state; nil keeps the current state.
	State json.RawMessage
}

// VirtualModelDefinition defines a virtual model.
type VirtualModelDefinition struct {
	Provider       string
	ID             string
	Name           string
	ThinkingLevels []string
	ContextWindow  int64
	MaxTokens      int64
	Input          []string
	Route          func(request ModelRouteRequest) (ModelRoute, error)
}

// modelThinkingLevels is the ordered thinking-level set of a virtual model.
var modelThinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

// IsVirtualModel reports whether a model is a virtual catalog entry.
func IsVirtualModel(model *ai.Model) bool {
	return model != nil && model.API == VirtualModelAPI
}

// FindLatestResponse is the latest successful assistant response; failed or
// aborted requests are skipped.
func FindLatestResponse(messages []ai.Message) *ai.AssistantMessage {
	for index := len(messages) - 1; index >= 0; index-- {
		assistant, ok := messages[index].(*ai.AssistantMessage)
		if !ok {
			continue
		}
		if assistant.StopReason != ai.StopError && assistant.StopReason != ai.StopAborted {
			return assistant
		}
	}
	return nil
}

type messageProbe struct {
	Role     string `json:"role"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	API      string `json:"api"`
}

func probeMessage(raw json.RawMessage) messageProbe {
	var probe messageProbe
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &probe)
	}
	return probe
}

// GetBranchSelection is the model selection a session branch records. A virtual
// model_change holds until the next model_change; otherwise the latest physical
// response wins. A virtual model that is no longer registered does not hold.
func GetBranchSelection(branch []SessionEntry, getModel func(provider, modelID string) *ai.Model) *ModelRouteSelection {
	for index := len(branch) - 1; index >= 0; index-- {
		entry := branch[index]
		if entry.Type == "model_change" {
			return &ModelRouteSelection{Provider: entry.Provider, ModelID: entry.ModelID}
		}
		if entry.Type != "message" {
			continue
		}
		probe := probeMessage(entry.Message)
		if probe.Role != "assistant" || probe.API == VirtualModelAPI {
			continue
		}
		response := &ModelRouteSelection{Provider: probe.Provider, ModelID: probe.Model}
		change := findLastModelChange(branch, index)
		if change == nil {
			return response
		}
		var model *ai.Model
		if getModel != nil {
			model = getModel(change.Provider, change.ModelID)
		}
		if model != nil && IsVirtualModel(model) {
			return change
		}
		return response
	}
	return nil
}

func findLastModelChange(branch []SessionEntry, before int) *ModelRouteSelection {
	for index := before - 1; index >= 0; index-- {
		entry := branch[index]
		if entry.Type == "model_change" {
			return &ModelRouteSelection{Provider: entry.Provider, ModelID: entry.ModelID}
		}
	}
	return nil
}

// GetVirtualModelState is the latest router state a branch stores for a virtual
// model.
func GetVirtualModelState(branch []SessionEntry, provider, modelID string) (json.RawMessage, bool) {
	for index := len(branch) - 1; index >= 0; index-- {
		entry := branch[index]
		if entry.Type != "custom" || entry.CustomType != VirtualModelStateEntry {
			continue
		}
		var data VirtualModelStateData
		if len(entry.Data) > 0 {
			if err := json.Unmarshal(entry.Data, &data); err != nil {
				continue
			}
		}
		if data.Provider == provider && data.ModelID == modelID {
			return data.State, true
		}
	}
	return nil, false
}

// CreateVirtualModel builds the catalog entry of a virtual model.
func CreateVirtualModel(definition VirtualModelDefinition) *ai.Model {
	levels := definition.ThinkingLevels
	if levels == nil {
		levels = []string{"off"}
	}
	included := map[string]bool{}
	for _, level := range levels {
		included[level] = true
	}
	thinkingLevelMap := ai.ThinkingLevelMap{}
	for _, level := range modelThinkingLevels {
		if included[level] {
			value := level
			thinkingLevelMap[level] = &value
		} else {
			thinkingLevelMap[level] = nil
		}
	}
	input := definition.Input
	if input == nil {
		input = []string{"text", "image"}
	}
	reasoning := false
	for _, level := range levels {
		if level != "off" {
			reasoning = true
			break
		}
	}
	return &ai.Model{
		ID: definition.ID, Name: definition.Name, API: VirtualModelAPI, Provider: definition.Provider,
		BaseURL: "", Reasoning: reasoning, ThinkingLevelMap: thinkingLevelMap, Input: input,
		Cost: ai.ModelCost{}, ContextWindow: definition.ContextWindow, MaxTokens: definition.MaxTokens,
	}
}
