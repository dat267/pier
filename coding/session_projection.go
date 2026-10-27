package coding

import (
	"encoding/json"
	"slices"

	"github.com/dat267/pier/ai"
)

// The session projection is what the next request carries: the entries on the
// path from the root to the current leaf, minus what a compaction replaced,
// decoded into LLM messages, plus the model and thinking level those messages
// were written under (upstream's buildSessionProjection).
//
// Callers used to assemble it by hand from buildSessionPath,
// applyCompactionWindow, getSessionContextSettings, getEntriesLocked and
// GetBranch. Nothing owned the answer, so each consumer picked a different
// subset, and the cost of a resolution was invisible at the call site: the
// cache-warmer currency check ended up projecting the whole session twice per
// request while holding the session lock. This module owns the answer, the
// cache, and the lock discipline.

// projectionVersion identifies a cached projection: the current leaf and the
// number of known entries. Any append, branch move, or load changes it.
type projectionVersion struct {
	leafID     string
	entryCount int
}

// projectionCache holds the projection resolved for a version. It is written
// and read under m.mu; the slices it carries are handed out with no spare
// capacity so a caller's append cannot write into the cache.
type projectionCache struct {
	valid   bool
	version projectionVersion
	context SessionContext
}

// Projection returns the LLM context for the current leaf, cached until the
// branch changes. Resolving decodes every message in the window, which is the
// expensive half of a request and of the footer's status changes.
func (m *SessionManager) Projection() SessionContext {
	m.mu.Lock()
	version := m.projectionVersionLocked()
	if m.projection.valid && m.projection.version == version {
		context := m.projection.context
		m.mu.Unlock()
		return context
	}
	// Resolve the branch by pointer under the lock. This used to copy every
	// entry into a fresh slice and build a fresh id map on every append, which
	// a 19k-entry session paid in full (11 ms of entry copy plus the map).
	// Only the entries the compaction window keeps are copied now, so the cost
	// tracks what the projection hands out, not the session size. Entries are
	// immutable after they are appended, so the pointers stay valid after the
	// lock is released.
	path := m.branchPointersLocked("")
	m.mu.Unlock()

	// Project outside the lock: decoding a large session takes tens of
	// milliseconds, and holding m.mu for it parks every other reader (the
	// footer reads the session per frame).
	context := buildSessionContextFromPointers(path, &m.messages)
	context.Messages = context.Messages[:len(context.Messages):len(context.Messages)]
	context.Entries = context.Entries[:len(context.Entries):len(context.Entries)]

	m.mu.Lock()
	// Only cache the resolution when the branch is still the one that was read;
	// a session swap during the projection must not be recorded under a version
	// the new session can reuse.
	if m.projectionVersionLocked() == version {
		m.projection = projectionCache{valid: true, version: version, context: context}
	}
	m.mu.Unlock()
	return context
}

// projectionVersionLocked is the cache key for the current branch.
func (m *SessionManager) projectionVersionLocked() projectionVersion {
	leafID := ""
	if m.leafID != nil {
		leafID = *m.leafID
	}
	return projectionVersion{leafID: leafID, entryCount: len(m.byID)}
}

// CurrentSystemMessage returns the system message the projection carries
// (upstream getCurrentSystemMessage(buildSessionProjection().messages)); the
// compaction entry and branch summaries record it.
func (m *SessionManager) CurrentSystemMessage() *ai.SystemMessage {
	return ai.GetCurrentSystemMessage(m.Projection().Messages)
}

// LatestCompaction returns the newest compaction entry on the current branch.
func (m *SessionManager) LatestCompaction() *SessionEntry {
	return GetLatestCompactionEntry(m.GetBranch(""))
}

// ProjectedEntry pairs an appended entry with the model-visible messages it
// contributes after context edits (upstream ProjectedSessionEntry). State-only
// entries and omissions carry no messages.
type ProjectedEntry struct {
	// Source is the raw append-only entry owning this contribution.
	Source *SessionEntry
	// Messages are the messages the entry contributes after context edits.
	Messages []ai.Message
}

// projectedWindow is a projection with provenance: the entries the compaction
// window keeps, the messages each contributes, the flattened messages, and the
// branch path the window was resolved on.
type projectedWindow struct {
	Entries  []ProjectedEntry
	Messages []ai.Message
	Branch   []*SessionEntry
}

// projectWindow resolves the compaction window on path and the messages each
// kept entry contributes (the entries half of upstream
// buildSessionProjection). A cache makes the walk read memoized messages
// instead of decoding the window again; nil is the reference path.
//
// buildSessionContextFromPointers derives the same messages with its own loop:
// that loop is the per-append path and keeps the flattening free of the
// provenance slice, so the two must stay in agreement
// (TestProjectedWindowMatchesSessionContext pins it).
func projectWindow(path []*SessionEntry, cache *messageCache) projectedWindow {
	var kept []*SessionEntry
	walkCompactionWindow(path, cache, func(entry *SessionEntry) { kept = append(kept, entry) })

	edits := map[string]*SessionEntry{}
	for _, entry := range kept {
		if entry.Type == "context_edit" && entry.TargetID != "" {
			edits[entry.TargetID] = entry
		}
	}

	window := projectedWindow{Entries: make([]ProjectedEntry, 0, len(kept)), Messages: make([]ai.Message, 0, len(kept)), Branch: path}
	for index, entry := range kept {
		var messages []ai.Message
		// The window may retain an older compaction whose raw ID lies inside the
		// newest retained range; only the newest compaction (index 0) contributes
		// a checkpoint and summary (upstream buildSessionProjection).
		if !(entry.Type == "compaction" && index > 0) {
			messages = projectedMessages(entry, cache)
			if edit := edits[entry.ID]; edit != nil {
				messages = applyContextEdit(messages, edit.Replacement)
			}
		}
		window.Entries = append(window.Entries, ProjectedEntry{Source: entry, Messages: messages})
		window.Messages = append(window.Messages, messages...)
	}
	return window
}

// MessageEntryID resolves the raw entry that persists a live message by
// identity (upstream _findPersistedMessageEntryId's reversed branch scan).
// Messages decoded for a projection share the cache's objects, so a resumed
// message installed from the projection matches its source entry here.
func (m *SessionManager) MessageEntryID(message ai.Message) string {
	m.mu.Lock()
	path := m.branchPointersLocked("")
	m.mu.Unlock()
	for _, entry := range slices.Backward(path) {

		if entry.Type != "message" {
			continue
		}
		if slices.Contains(m.messages.messages(entry), message) {
			return entry.ID
		}
	}
	return ""
}

// ProjectedEntries returns the projection's per-entry contributions (upstream
// buildSessionProjection().entries): what each kept entry adds to the context
// after context edits.
func (m *SessionManager) ProjectedEntries() []ProjectedEntry {
	m.mu.Lock()
	path := m.branchPointersLocked("")
	m.mu.Unlock()
	return projectWindow(path, &m.messages).Entries
}

// ProjectedContextEstimate estimates the tokens the current projection carries
// (port of estimateProjectedContextTokens). Callers use it instead of raw
// message sizes when a context edit may have changed what the model sees.
func (m *SessionManager) ProjectedContextEstimate() CompactionContextUsageEstimate {
	m.mu.Lock()
	path := m.branchPointersLocked("")
	m.mu.Unlock()
	return EstimateProjectedContextTokens(projectWindow(path, &m.messages))
}

// HasContextEdits reports whether the current projection carries a context edit
// (upstream projection.entries.some(sourceEntry.type === "context_edit")). An
// edit changes the context without producing provider usage, so usage-based
// accounting cannot be trusted for the branch.
func (m *SessionManager) HasContextEdits() bool {
	for _, entry := range m.Projection().Entries {
		if entry.Type == "context_edit" {
			return true
		}
	}
	return false
}

// ContextSignature summarizes branch metadata without decoding messages. Cache
// warming checks the live agent's message-reference prefix instead: counts
// alone cannot establish whether the warmed context is still current.
type ContextSignature struct {
	Provider     string
	ModelID      string
	MessageCount int
}

// ContextSignature returns the current context signature cheaply.
func (m *SessionManager) ContextSignature() ContextSignature {
	m.mu.Lock()
	branch := m.branchPointersLocked("")
	m.mu.Unlock()
	return contextSignatureFrom(branch)
}

// contextSignatureFrom resolves the signature from an already-resolved path of
// pointers, walking the compaction window without materializing it.
func contextSignatureFrom(path []*SessionEntry) ContextSignature {
	count := 0
	walkCompactionWindow(path, nil, func(entry *SessionEntry) {
		if contextEntryYieldsMessage(entry) {
			count++
		}
	})
	provider, modelID := contextModelRef(path)
	return ContextSignature{Provider: provider, ModelID: modelID, MessageCount: count}
}

// contextEntryYieldsMessage reports whether projecting the entry produces a
// message (sessionEntryToContextMessages; a message entry that fails to
// unmarshal is still counted).
func contextEntryYieldsMessage(entry *SessionEntry) bool {
	switch entry.Type {
	case "message", "custom_message", "compaction":
		return true
	case "branch_summary":
		return entry.Summary != ""
	}
	return false
}

// contextModelRef returns the model the context uses. getSessionContextSettings
// scans the path forward and lets later entries win, so the answer is the last
// model_change entry or assistant message.
func contextModelRef(path []*SessionEntry) (provider string, modelID string) {
	for _, entry := range slices.Backward(path) {

		switch entry.Type {
		case "model_change":
			return entry.Provider, entry.ModelID
		case "message":
			var message struct {
				Role     string `json:"role"`
				Provider string `json:"provider"`
				Model    string `json:"model"`
			}
			if json.Unmarshal(entry.Message, &message) == nil && message.Role == "assistant" {
				return message.Provider, message.Model
			}
		}
	}
	return "", ""
}
