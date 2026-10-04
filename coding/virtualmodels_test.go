package coding

import (
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// Port of core/virtual-models.ts.

func TestCreateVirtualModel(t *testing.T) {
	model := CreateVirtualModel(VirtualModelDefinition{
		Provider: "anthropic", ID: "auto", Name: "Auto", ThinkingLevels: []string{"off", "high"},
		ContextWindow: 100, MaxTokens: 50,
	})
	if model.ID != "auto" || model.Name != "Auto" || model.Provider != "anthropic" || model.API != VirtualModelAPI {
		t.Fatalf("model = %+v", model)
	}
	if !model.Reasoning {
		t.Fatal("a non-off thinking level means reasoning")
	}
	if model.ThinkingLevelMap["off"] == nil || model.ThinkingLevelMap["high"] == nil {
		t.Fatalf("map = %+v", model.ThinkingLevelMap)
	}
	if model.ThinkingLevelMap["low"] != nil {
		t.Fatalf("map = %+v", model.ThinkingLevelMap)
	}
	if len(model.Input) != 2 || model.Input[0] != "text" || model.Input[1] != "image" {
		t.Fatalf("input = %+v", model.Input)
	}
	if model.ContextWindow != 100 || model.MaxTokens != 50 {
		t.Fatalf("limits = %d, %d", model.ContextWindow, model.MaxTokens)
	}
	// Defaults: off only, not reasoning.
	plain := CreateVirtualModel(VirtualModelDefinition{Provider: "p", ID: "i", Name: "n"})
	if plain.Reasoning || plain.ThinkingLevelMap["off"] == nil || len(plain.ThinkingLevelMap) != len(modelThinkingLevels) {
		t.Fatalf("plain = %+v", plain)
	}
	if !IsVirtualModel(plain) || IsVirtualModel(&ai.Model{API: "anthropic"}) {
		t.Fatal("IsVirtualModel")
	}
}

func TestFindLatestResponse(t *testing.T) {
	messages := []ai.Message{
		&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}},
		&ai.AssistantMessage{StopReason: ai.StopStop, Timestamp: 1},
		&ai.AssistantMessage{StopReason: ai.StopError, Timestamp: 2},
		&ai.AssistantMessage{StopReason: ai.StopAborted, Timestamp: 3},
	}
	response := FindLatestResponse(messages)
	if response == nil || response.Timestamp != 1 {
		t.Fatalf("response = %+v", response)
	}
	if FindLatestResponse([]ai.Message{&ai.AssistantMessage{StopReason: ai.StopError}}) != nil {
		t.Fatal("a failed response is skipped")
	}
}

func TestGetBranchSelection(t *testing.T) {
	physical := &ai.Model{ID: "sonnet", Provider: "anthropic", API: "anthropic-messages"}
	virtual := &ai.Model{ID: "auto", Provider: "anthropic", API: VirtualModelAPI}
	getModel := func(provider, modelID string) *ai.Model {
		if modelID == "auto" {
			return virtual
		}
		return physical
	}
	message := func(provider, modelID string) SessionEntry {
		return SessionEntry{Type: "message",
			Message: json.RawMessage(`{"role":"assistant","provider":"` + provider + `","model":"` + modelID + `"}`)}
	}
	// A physical model_change followed by a response keeps the response.
	branch := []SessionEntry{
		{Type: "model_change", Provider: "anthropic", ModelID: "sonnet"},
		message("anthropic", "sonnet"),
	}
	selection := GetBranchSelection(branch, getModel)
	if selection == nil || selection.Provider != "anthropic" || selection.ModelID != "sonnet" {
		t.Fatalf("selection = %+v", selection)
	}
	// A virtual model_change holds until the next model_change.
	branch = []SessionEntry{
		{Type: "model_change", Provider: "anthropic", ModelID: "auto"},
		message("anthropic", "sonnet"),
	}
	selection = GetBranchSelection(branch, getModel)
	if selection == nil || selection.ModelID != "auto" {
		t.Fatalf("selection = %+v", selection)
	}
	// An unregistered virtual model does not hold.
	selection = GetBranchSelection(branch, func(provider, modelID string) *ai.Model { return nil })
	if selection == nil || selection.ModelID != "sonnet" {
		t.Fatalf("selection = %+v", selection)
	}
	// A model_change with no response returns the change.
	selection = GetBranchSelection([]SessionEntry{{Type: "model_change", Provider: "p", ModelID: "m"}}, getModel)
	if selection == nil || selection.Provider != "p" || selection.ModelID != "m" {
		t.Fatalf("selection = %+v", selection)
	}
	if GetBranchSelection(nil, getModel) != nil {
		t.Fatal("an empty branch has no selection")
	}
}

func TestGetVirtualModelState(t *testing.T) {
	branch := []SessionEntry{
		{Type: "custom", CustomType: VirtualModelStateEntry,
			Data: json.RawMessage(`{"provider":"anthropic","modelId":"auto","state":{"count":1}}`)},
		{Type: "message", Message: json.RawMessage(`{"role":"user"}`)},
		{Type: "custom", CustomType: VirtualModelStateEntry,
			Data: json.RawMessage(`{"provider":"anthropic","modelId":"auto","state":{"count":2}}`)},
	}
	state, present := GetVirtualModelState(branch, "anthropic", "auto")
	if !present || string(state) != `{"count":2}` {
		t.Fatalf("state = %s, %v", state, present)
	}
	if _, present := GetVirtualModelState(branch, "other", "auto"); present {
		t.Fatal("another provider has no state")
	}
}
