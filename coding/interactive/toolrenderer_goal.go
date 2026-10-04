package interactive

// Port of the goal extension's tool renderers (render.ts): the get_goal,
// create_goal and update_goal call/result cards, registered alongside the
// built-in tool renderers so a goal tool call gets a message frame.

import (
	"encoding/json"

	"github.com/dat267/pier/goal"
	"github.com/dat267/pier/tui"
)

// goalDetailsFromResult decodes the goal payload the tools attach to Details
// ({goal: <GoalView-ish>} for get_goal; {goal: {phase}} for update_goal).
func goalDetailsFromResult(result *SortToolResultContent) (json.RawMessage, bool) {
	if result == nil || result.Details == nil {
		return nil, false
	}
	switch typed := result.Details.(type) {
	case []byte:
		return typed, true
	case string:
		return json.RawMessage(typed), true
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

// goalPhaseFromDetails decodes an update_goal result's {goal: {phase}} payload.
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

// goalCreateRenderers renders the create_goal tool (no result card upstream;
// the generic result body stands in).
var goalCreateRenderers = ToolRenderers{
	RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
		objective, _ := argString(toolArgs(args), "objective")
		return goal.RenderCreateGoalRenderCall(objective, theme)
	},
}

// goalUpdateRenderers renders the update_goal tool.
var goalUpdateRenderers = ToolRenderers{
	RenderCall: func(args any, theme *Theme, context *ToolRenderContext) tui.Component {
		argsMap := toolArgs(args)
		action, _ := argString(argsMap, "action")
		blockedReason, _ := argString(argsMap, "blocked_reason")
		return goal.RenderUpdateGoalRenderCall(action, blockedReason, theme)
	},
	RenderResult: func(result *SortToolResultContent, options ToolRenderResultOptions, theme *Theme, context *ToolRenderContext) tui.Component {
		out := goal.UpdateGoalResult{IsError: result.IsError}
		if text := GetTextOutput(result, context.ShowImages); text != "" {
			out.Text = text
		}
		if details, ok := goalDetailsFromResult(result); ok {
			if phase, has := goalPhaseFromDetails(details); has {
				out.Phase = phase
				out.HasPhase = true
			}
		}
		return goal.RenderUpdateGoalRenderResult(out, theme)
	},
}
