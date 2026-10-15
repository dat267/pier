package interactive

import (
	"encoding/json"

	"github.com/dat267/pier/internal/goal"
	"github.com/dat267/pier/tui"
)

// Port of the goal extension's transcript entry renderers: the durable
// lifecycle (pi-goal) and admitted-round (pi-goal-turn) cards, click-to-expand
// like the extension's registerEntryRenderer wrappers.

// GoalEntryRenderer resolves the transcript renderers for the goal custom
// entries.
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
			return goal.ClickToExpand(func(expanded bool) tui.Component {
				return goal.RenderGoalChangeEntry(change, theme, expanded)
			}, options.Expanded)
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
			return goal.ClickToExpand(func(expanded bool) tui.Component {
				return goal.RenderGoalTurnEntry(turn, theme, expanded)
			}, options.Expanded)
		}
	}
	return nil
}
