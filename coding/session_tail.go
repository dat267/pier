package coding

import (
	"errors"

	"github.com/dat267/pier/ai"
)

// TailSession writes a new session that starts from the current live context
// and opens it. Pier-only (D378): upstream has no equivalent command, only the
// `setup(sessionManager)` hook on newSession that extension code uses to seed a
// session, and extension mechanics are out of scope here (D41).
//
// The live context is the same window `ProjectedContextEstimate` reads: the
// newest compaction plus the entries its window keeps, including context edits.
// Copying those entries re-rooted onto fresh model/thinking entries reproduces
// the projection exactly while dropping the cold prefix, so a long session's
// history stops traveling with every resume. Without a compaction there is no
// window to cut, so the whole branch is copied and the new session still matches
// the source context.
func TailSession(source *SessionManager, model *ai.Model, thinking ai.ThinkingLevel, targetCwd string, sessionDir string, manageDir bool, options *NewSessionOptions) (*SessionManager, error) {
	if source == nil {
		return nil, errors.New("Nothing to hand off yet")
	}
	branch := source.GetBranch("")
	if len(branch) == 0 {
		return nil, errors.New("Nothing to hand off yet")
	}

	window := tailWindow(branch)
	hasConversation := false
	for i := range window {
		if window[i].Type == "message" && projectedRole(&window[i], nil) != string(ai.RoleSystem) {
			hasConversation = true
			break
		}
	}
	if !hasConversation {
		// A fresh session already holds model/thinking bookkeeping and its system
		// prompt, so "no leaf" is not enough to tell there is no conversation yet.
		return nil, errors.New("Nothing to tail yet")
	}
	hasModelChange, hasThinkingChange := false, false
	for _, entry := range window {
		switch entry.Type {
		case "model_change":
			hasModelChange = true
		case "thinking_level_change":
			hasThinkingChange = true
		}
	}

	entries := make([]SessionEntry, 0, len(window)+2)
	if model != nil && !hasModelChange {
		entries = append(entries, newSessionEntry("model_change"))
		entries[len(entries)-1].Provider = model.Provider
		entries[len(entries)-1].ModelID = model.ID
	}
	if thinking != "" && !hasThinkingChange {
		entries = append(entries, newSessionEntry("thinking_level_change"))
		entries[len(entries)-1].ThinkingLevel = string(thinking)
	}
	entries = append(entries, window...)
	for i := range entries {
		if i == 0 {
			entries[i].ParentID = nil
			continue
		}
		parent := entries[i-1].ID
		entries[i].ParentID = &parent
	}

	parentSession := source.GetSessionFile()
	var parent *string
	if parentSession != "" {
		parent = &parentSession
	}
	return writeForkedSession(targetCwd, sessionDir, manageDir, parent, entries, options)
}

// tailWindow returns the entries the live context keeps, in branch order:
// the newest compaction, the entries its window keeps before it (system messages
// are not replayed there, matching walkCompactionWindow), and everything after
// it. The compaction's first kept entry stays valid because IDs are preserved.
func tailWindow(branch []SessionEntry) []SessionEntry {
	compactionIndex := -1
	for i := range branch {
		if branch[i].Type == "compaction" {
			compactionIndex = i
		}
	}
	if compactionIndex < 0 {
		return append([]SessionEntry{}, branch...)
	}

	compaction := branch[compactionIndex]
	start := compactionIndex
	for i := 0; i < compactionIndex; i++ {
		if branch[i].ID == compaction.FirstKeptEntryID {
			start = i
			break
		}
	}
	window := make([]SessionEntry, 0, compactionIndex-start+1+len(branch)-compactionIndex)
	for i := start; i < compactionIndex; i++ {
		entry := &branch[i]
		if entry.Type == "message" && projectedRole(entry, nil) == string(ai.RoleSystem) {
			continue
		}
		window = append(window, *entry)
	}
	kept := compaction
	if start == compactionIndex || branch[start].ID != compaction.FirstKeptEntryID {
		// The first kept entry was outside the branch (or the compaction pointed
		// at itself); a window without it replays as the compaction alone.
		kept.FirstKeptEntryID = kept.ID
	}
	window = append(window, kept)
	window = append(window, branch[compactionIndex+1:]...)
	return window
}

// newSessionEntry builds a fresh entry the way the session appends build them.
func newSessionEntry(entryType string) SessionEntry {
	return SessionEntry{Type: entryType, SessionEntryBase: SessionEntryBase{ID: UUIDv7(), Timestamp: sessionTimestamp()}}
}
