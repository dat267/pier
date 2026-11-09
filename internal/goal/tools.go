// This file holds the goal's tool-exposure policy. It is pure so the session
// can apply it without knowing the phase rules, and so the rules stay next to
// the state machine that drives them.
package goal

// DesiredToolNames returns the active tool names the goal feature exposes,
// given the current list. Both tools stay available for the whole session so
// the goal phase never changes the request prefix (D418): a provider caches the
// prefix, and the tool array plus the system prompt's tools and rules sections
// sit at its front, so exposing get_goal only while a goal is pursued rewrote
// both on every create, complete, pause and resume. get_goal reports that there
// is no goal when that is the case. The current order is preserved and missing
// names append.
func DesiredToolNames(current []string) []string {
	seen := map[string]bool{}
	next := make([]string, 0, len(current)+2)
	for _, name := range current {
		if !seen[name] {
			next = append(next, name)
			seen[name] = true
		}
	}
	for _, name := range []string{"set_goal", "get_goal"} {
		if !seen[name] {
			next = append(next, name)
			seen[name] = true
		}
	}
	return next
}
