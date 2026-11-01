package interactive

import (
	"encoding/json"
	"strings"

	"github.com/dat267/pier/coding"
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

// goalActionCards coalesces a successful objective tool's durable cards into
// its tool frame (D223). Entries stay visible until success identifies the exact
// created goal, so errors and unrelated command changes keep their own cards.
// This state is owner-only and retained only while objective tools are pending.
type goalActionCards struct {
	calls   map[string]bool
	created map[string][]*CustomEntryComponent
	clear   *CustomEntryComponent
	clearID string
}

func (g *goalActionCards) start(name, id string, args any) {
	if name != "set_goal" {
		return
	}
	objective, _ := argString(toolArgs(args), "objective")
	if objective == "" {
		return
	}
	if g.calls == nil {
		g.calls = make(map[string]bool)
	}
	g.calls[id] = true
}

func (g *goalActionCards) record(entry *coding.SessionEntry, card *CustomEntryComponent) {
	if len(g.calls) == 0 || entry.CustomType != goal.GoalCustomType {
		return
	}
	var change goal.GoalChangeEntry
	if json.Unmarshal(entry.Data, &change) != nil {
		return
	}
	if change.Operation == goal.OpCreate && change.Goal != nil {
		if g.created == nil {
			g.created = make(map[string][]*CustomEntryComponent)
		}
		cards := []*CustomEntryComponent{card}
		if g.clear != nil && entry.ParentID != nil && *entry.ParentID == g.clearID {
			cards = append(cards, g.clear)
		}
		g.created[change.Goal.ID] = cards
	}
	g.clear, g.clearID = nil, ""
	if change.Operation == goal.OpClear {
		g.clear, g.clearID = card, entry.ID
	}
}

func (g *goalActionCards) finish(id string, result *SortToolResultContent, chat *tui.Container) {
	if !g.calls[id] {
		return
	}
	if !result.IsError {
		if details, ok := goalDetailsFromResult(result); ok {
			if view := goalViewFromDetails(details); view != nil {
				cards := g.created[view.ID]
				if len(cards) > 0 {
					chat.RemoveChild(cards[0])
				}
				// A plain creation may follow an independent /goal clear.
				// Only replacement owns the preceding tombstone card (D233).
				if len(cards) > 1 && strings.HasPrefix(GetTextOutput(result, false), "Goal replaced") {
					chat.RemoveChild(cards[1])
				}
				delete(g.created, view.ID)
			}
		}
	}
	delete(g.calls, id)
	if len(g.calls) == 0 {
		*g = goalActionCards{}
	}
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
