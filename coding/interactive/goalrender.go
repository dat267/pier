package interactive

import (
	"encoding/json"

	"github.com/dat267/pier/goal"
	"github.com/dat267/pier/tui"
)

// GoalEntryRenderer resolves the transcript renderers for the goal custom
// entries (port of the goal extension's registerEntryRenderer calls).
func GoalEntryRenderer(customType string) EntryRenderer {
	switch customType {
	case goal.GoalCustomType:
		return func(entry CustomEntry, options EntryRenderOptions, theme *Theme) tui.Component {
			raw, ok := entry.Data.(json.RawMessage)
			if !ok {
				return nil
			}
			var change goal.GoalChangeEntry
			if err := json.Unmarshal(raw, &change); err != nil {
				return nil
			}
			return goal.RenderGoalChangeEntry(change, theme, options.Expanded)
		}
	case goal.GoalTurnType:
		return func(entry CustomEntry, options EntryRenderOptions, theme *Theme) tui.Component {
			raw, ok := entry.Data.(json.RawMessage)
			if !ok {
				return nil
			}
			var turn goal.GoalTurnEntry
			if err := json.Unmarshal(raw, &turn); err != nil {
				return nil
			}
			return goal.RenderGoalTurnEntry(turn, theme, options.Expanded)
		}
	}
	return nil
}
