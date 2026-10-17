package interactive

// Port of the goal extension's tool renderers: the get_goal and set_goal
// call/result cards, registered alongside the built-in tool renderers so a goal
// tool call gets a message frame.

import (
	"encoding/json"

	"github.com/dat267/pier/internal/goal"
	"github.com/dat267/pier/tui"
)

// goalDetailsFromResult decodes the goal payload the tools attach to Details
// ({goal: <GoalView-ish>} for get_goal; {goal: {phase}} for set_goal).
func goalDetailsFromResult(result *SortToolResultContent) (json.RawMessage, bool) {
	if result == nil || result.Details == nil {
		return nil, false
	}
	switch typed := result.Details.(type) {
	case []byte:
		return typed, true
	case string:
		return json.RawMessage(typed), true
	case json.RawMessage:
		return typed, true
	}
	encoded, err := json.Marshal(result.Details)
	if err != nil {
		return nil, false
	}
	return encoded, true
}

// goalViewFromDetails decodes a get_goal result's {goal: GoalView} payload.
func goalViewFromDetails(details json.RawMessage) *goal.GoalView {
	var envelope struct {
		Goal *goal.GoalView `json:"goal"`
	}
	if err := json.Unmarshal(details, &envelope); err != nil {
		return nil
	}
	return envelope.Goal
}

// goalPhaseFromDetails decodes a set_goal result's {goal: {phase}} payload.
func goalPhaseFromDetails(details json.RawMessage) (goal.GoalPhase, bool) {
	var envelope struct {
		Goal *struct {
			Phase goal.GoalPhase `json:"phase"`
		} `json:"goal"`
	}
	if err := json.Unmarshal(details, &envelope); err != nil || envelope.Goal == nil {
		return "", false
	}
	return envelope.Goal.Phase, true
}

// goalActionFromDetails decodes a set_goal result's status (D232).
func goalActionFromDetails(details json.RawMessage) string {
	var envelope struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(details, &envelope); err != nil {
		return ""
	}
	return envelope.Action
}

// goalGetRenderers renders the get_goal tool.
var goalGetRenderers = ToolRenderers{
	RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
		return goal.RenderGetGoalRenderCall(theme)
	},
	RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
		details, ok := goalDetailsFromResult(result)
		if !ok {
			return goal.RenderGetGoalRenderResult(nil, theme)
		}
		return goal.RenderGetGoalRenderResult(goalViewFromDetails(details), theme)
	},
}

// goalSetRenderers renders the set_goal tool: an objective call sets/replaces
// the goal, a status call ends it. A status or error result draws a phase card;
// a successful objective set has no result card (upstream create_goal).
var goalSetRenderers = ToolRenderers{
	RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
		argsMap := toolArgs(args)
		objective, _ := argString(argsMap, "objective")
		status, _ := argString(argsMap, "status")
		blockedReason, _ := argString(argsMap, "blocked_reason")
		if status != "" {
			return goal.RenderSetGoalStatusRenderCall(status, blockedReason, theme, context.Expanded)
		}
		return goal.RenderSetGoalRenderCall(objective, theme, context.Expanded)
	},
	RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
		out := goal.SetGoalResult{IsError: result.IsError}
		if text := GetTextOutput(result, context.ShowImages); text != "" {
			out.Text = text
		}
		action := ""
		if details, ok := goalDetailsFromResult(result); ok {
			action = goalActionFromDetails(details)
			out.Action = action
			if phase, has := goalPhaseFromDetails(details); has {
				out.Phase = phase
				out.HasPhase = true
			}
		}
		if !result.IsError && action != "complete" && action != "blocked" && action != "clear" {
			// An objective set has no result card; the generic body shows the reply.
			return nil
		}
		return goal.RenderSetGoalRenderResult(out, theme)
	},
}
