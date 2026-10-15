package interactive

import (
	"encoding/json"

	"github.com/dat267/pier/internal/goal"
	"github.com/dat267/pier/tui"
)

// Port of the goal extension's transcript entry renderers: the durable
// lifecycle (pi-goal) and admitted-round (pi-goal-turn) cards, click-to-expand
// like the extension's registerEntryRenderer wrappers.

// goalEntryRaw normalizes a custom entry's data payload (the transcript hands
// out json.RawMessage; tests and other callers may pass []byte).
func goalEntryRaw(data any) (json.RawMessage, bool) {
	switch typed := data.(type) {
	case json.RawMessage:
		return typed, true
	case []byte:
		return typed, true
	case string:
		return json.RawMessage(typed), true
	}
	return nil, false
}

// GoalEntryRenderer resolves the transcript renderers for the goal custom
// entries.
func GoalEntryRenderer(customType string) EntryRenderer {
	switch customType {
	case goal.GoalCustomType:
		return func(entry CustomEntry, options EntryRenderOptions, theme *Theme) tui.Component {
			raw, ok := goalEntryRaw(entry.Data)
			if !ok {
				return nil
			}
			var change goal.GoalChangeEntry
			if err := json.Unmarshal(raw, &change); err != nil {
				return nil
			}
			return goal.ClickToExpand(func(expanded bool) tui.Component {
				return goal.RenderGoalChangeEntry(change, theme, expanded)
			}, options.Expanded)
		}
	case goal.GoalTurnType:
		return func(entry CustomEntry, options EntryRenderOptions, theme *Theme) tui.Component {
			raw, ok := goalEntryRaw(entry.Data)
			if !ok {
				return nil
			}
			var turn goal.GoalTurnEntry
			if err := json.Unmarshal(raw, &turn); err != nil {
				return nil
			}
			return goal.ClickToExpand(func(expanded bool) tui.Component {
				return goal.RenderGoalTurnEntry(turn, theme, expanded)
			}, options.Expanded)
		}
	}
	return nil
}
