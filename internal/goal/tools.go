// This file holds the goal's tool-exposure policy. It is pure so the session
// can apply it without knowing the phase rules, and so the rules stay next to
// the state machine that drives them.
package goal

// DesiredToolNames returns the active tool names the goal feature exposes,
// given the current list and whether a goal is being pursued. set_goal stays
// available so the model can be asked to set one; get_goal only means
// something while a goal is active. The current order is preserved and missing
// names append.
func DesiredToolNames(current []string, pursuing bool) []string {
	seen := map[string]bool{}
	next := make([]string, 0, len(current)+2)
	for _, name := range current {
		if !seen[name] {
			next = append(next, name)
			seen[name] = true
		}
	}
	if !seen["set_goal"] {
		next = append(next, "set_goal")
		seen["set_goal"] = true
	}
	if pursuing && !seen["get_goal"] {
		next = append(next, "get_goal")
		seen["get_goal"] = true
	}
	if !pursuing {
		filtered := next[:0]
		for _, name := range next {
			if name != "get_goal" {
				filtered = append(filtered, name)
			}
		}
		next = filtered
	}
	return next
}
