package ai

import "sync"

// Port of pi v1.5.2 (b4afdddf) api/strict-tool-limits.ts.
//
// Claude rejects, on the Anthropic API and on Bedrock, a request whose strict
// tools exceed its limits (observed: 20 strict tools, 16 optional or union
// parameters across all strict tools). A rejected request is retried once
// without strict tools inside the same call; the session is then remembered, so
// its later requests send preferred strict tools non-strict right away. Trying
// strict again would only repeat the rejection, and returning to strict later
// would rewrite the tool definitions and miss the prompt cache.

// maxSessionsOverStrictToolLimits bounds the memory of sessions that are never
// cleaned up. A dropped session just pays one more rejection.
const maxSessionsOverStrictToolLimits = 10_000

var (
	strictToolLimitMu    sync.Mutex
	strictToolLimitSet   = map[string]bool{}
	strictToolLimitOrder []string
)

// MarkStrictToolLimitsExceeded records that a session's strict tools exceeded
// Claude's limits. Without a session it does nothing.
func MarkStrictToolLimitsExceeded(sessionID string) {
	if sessionID == "" {
		return
	}
	strictToolLimitMu.Lock()
	defer strictToolLimitMu.Unlock()
	if !strictToolLimitSet[sessionID] {
		strictToolLimitSet[sessionID] = true
		strictToolLimitOrder = append(strictToolLimitOrder, sessionID)
	}
	for len(strictToolLimitOrder) > maxSessionsOverStrictToolLimits {
		oldest := strictToolLimitOrder[0]
		strictToolLimitOrder = strictToolLimitOrder[1:]
		delete(strictToolLimitSet, oldest)
	}
}

// HasExceededStrictToolLimits reports whether an earlier request of this
// session had its strict tools rejected for Claude's limits.
func HasExceededStrictToolLimits(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	strictToolLimitMu.Lock()
	defer strictToolLimitMu.Unlock()
	return strictToolLimitSet[sessionID]
}

// ResetStrictToolLimits forgets one session's entry, or every entry when the
// session id is empty. CleanupSessionResources calls it when a session ends.
func ResetStrictToolLimits(sessionID string) {
	strictToolLimitMu.Lock()
	defer strictToolLimitMu.Unlock()
	if sessionID == "" {
		strictToolLimitSet = map[string]bool{}
		strictToolLimitOrder = nil
		return
	}
	if !strictToolLimitSet[sessionID] {
		return
	}
	delete(strictToolLimitSet, sessionID)
	for index, candidate := range strictToolLimitOrder {
		if candidate == sessionID {
			strictToolLimitOrder = append(strictToolLimitOrder[:index], strictToolLimitOrder[index+1:]...)
			break
		}
	}
}
