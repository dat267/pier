package coding

import (
	"bytes"
	"context"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// Port of the virtual-selection branch of pi v1.5.2 (b4afdddf)
// core/agent-session.ts prepareRequest: the run keeps the virtual model as its
// selection, and each request is routed to a physical model for that request
// only. Router state that changes lands on the session branch as a
// pi.virtual-model-state custom entry, which the next request reads back.

// prepareVirtualRequest routes one request of a virtual selection. It returns
// nil for a physical selection, which leaves the request untouched.
func (s *AgentSession) prepareVirtualRequest(request *agent.PrepareRequestContext, ctx context.Context) (*agent.AgentRequestUpdate, error) {
	if s == nil || request == nil || !IsVirtualModel(request.Model) {
		return nil, nil
	}
	runtime := s.modelRuntime()
	if runtime == nil {
		return nil, nil
	}
	model := request.Model
	messages := s.convertToLlm(request.Context.Messages)
	failure := s.takeRouteFailure()
	reason := "continuation"
	switch {
	case failure != nil:
		reason = "retry"
	case hasUserTurnAfterLastAssistant(messages):
		// Only messages the user wrote start a turn; extension messages can
		// follow them.
		reason = "user"
	}
	state, _ := GetVirtualModelState(s.Sessions.GetBranch(""), model.Provider, model.ID)
	route, err := runtime.ResolveVirtualModel(model, messages, VirtualModelResolveOptions{
		Reason:        reason,
		ThinkingLevel: request.ThinkingLevel,
		Failed:        failure,
		State:         state,
		Signal:        ctx,
	})
	if err != nil {
		return nil, err
	}
	if len(route.State) > 0 && !bytes.Equal(route.State, state) {
		data, marshalErr := ai.MarshalJSON(VirtualModelStateData{
			Provider: model.Provider, ModelID: model.ID, State: route.State,
		})
		if marshalErr == nil {
			if id := s.Sessions.AppendCustomEntry(VirtualModelStateEntry, data); id != "" {
				if entry := s.Sessions.GetEntry(id); entry != nil {
					s.emit(&SessionEvent{Type: SessionEntryAppended, Entry: entry})
				}
			}
		}
	}
	return &agent.AgentRequestUpdate{
		RequestModel:            route.Model,
		RequestThinkingLevel:    ai.ThinkingLevel(route.ThinkingLevel),
		HasRequestThinkingLevel: true,
	}, nil
}

// hasUserTurnAfterLastAssistant reports whether the conversation's last turn
// starts with a user message, which is upstream's userTurn test.
func hasUserTurnAfterLastAssistant(messages []ai.Message) bool {
	lastAssistant := -1
	for index, message := range messages {
		if _, ok := message.(*ai.AssistantMessage); ok {
			lastAssistant = index
		}
	}
	for _, message := range messages[lastAssistant+1:] {
		if _, ok := message.(*ai.UserMessage); ok {
			return true
		}
	}
	return false
}

// modelRuntime returns the configured runtime, if any.
func (s *AgentSession) modelRuntime() *ModelRuntime {
	if s.control == nil {
		return nil
	}
	return s.control.ModelRuntime
}

// setRouteFailure records the response a retry replaces, so the next routed
// request reports reason "retry" with the failed model.
func (s *AgentSession) setRouteFailure(message *ai.AssistantMessage) {
	s.mu.Lock()
	s.routeFailure = message
	s.mu.Unlock()
}

// takeRouteFailure reads and clears the recorded failure.
func (s *AgentSession) takeRouteFailure() *ai.AssistantMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	failure := s.routeFailure
	s.routeFailure = nil
	return failure
}

// routeSummaryModel routes a virtual selection for a summary request, which
// upstream routes with reason "direct" and no stored state. It returns the model
// and level to use, unchanged for a physical selection.
func (s *AgentSession) routeSummaryModel(ctx context.Context, model *ai.Model, level string) (*ai.Model, string, error) {
	if !IsVirtualModel(model) {
		return model, level, nil
	}
	runtime := s.modelRuntime()
	if runtime == nil {
		return model, level, nil
	}
	route, err := runtime.ResolveVirtualModel(model, s.convertToLlm(s.Agent.State().Messages), VirtualModelResolveOptions{
		Reason: "direct", ThinkingLevel: level, Signal: ctx,
	})
	if err != nil {
		return nil, "", err
	}
	return route.Model, route.ThinkingLevel, nil
}
