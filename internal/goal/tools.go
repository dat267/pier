// This file holds the goal's tool-exposure policy. It is pure so the session
// can apply it without knowing the phase rules, and so the rules stay next to
// the state machine that drives them.
package goal

// GoalToolNames are the goal tool names in registration order.
var GoalToolNames = []string{"get_goal", "create_goal", "update_goal"}

// DesiredToolNames returns the active tool names the goal feature exposes,
// given the current list and whether a goal is being pursued. create_goal stays
// available so the model can be asked to set one; get_goal/update_goal only
// mean something while a goal is active. The current order is preserved and
// missing names append.
func DesiredToolNames(current []string, pursuing bool) []string {
	seen := map[string]bool{}
	next := make([]string, 0, len(current)+3)
	for _, name := range current {
		if !seen[name] {
			next = append(next, name)
			seen[name] = true
		}
	}
	if !seen["create_goal"] {
		next = append(next, "create_goal")
		seen["create_goal"] = true
	}
	for _, name := range []string{"get_goal", "update_goal"} {
		if pursuing {
			if !seen[name] {
				next = append(next, name)
				seen[name] = true
			}
			continue
		}
		delete(seen, name)
	}
	if !pursuing {
		filtered := next[:0]
		for _, name := range next {
			if name != "get_goal" && name != "update_goal" {
				filtered = append(filtered, name)
			}
		}
		next = filtered
	}
	return next
}
