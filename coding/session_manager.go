package coding

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/offloop"
)

// Port of the SessionManager class: append-only JSONL session trees.

// SessionManager manages conversation sessions as append-only trees stored
// in JSONL files. Each entry has an id and parentId forming a tree; the leaf
// pointer tracks the current position. Appending creates a child of the
// current leaf; branching moves the leaf to an earlier entry.
type SessionManager struct {
	mu sync.Mutex

	sessionID   string
	sessionFile string
	sessionDir  string
	cwd         string
	persist     bool
	// writeStarted tracks admission of the initial rewrite, not disk success.
	writeStarted bool
	writeState   *sessionWriteState

	// writeQueue, when wired, carries the file writes (offloop); nil keeps
	// them synchronous. See SessionManagerOptions.WriteQueue.
	writeQueue *offloop.Queue

	// writeErrorsMu protects persistence diagnostics, not manager/UI state.
	// Workers report results without acquiring mu or invoking callbacks.
	writeErrorsMu    sync.Mutex
	writeErrors      []SessionWriteError
	writeFailures    map[string]error
	writeErrorsReady chan struct{}

	fileEntries []FileEntry

	// cacheScan seeding: the loaded prefix is consumed by a background
	// goroutine (it parses every message's JSON, ~250ms on a 55MB session),
	// while appends queue until the seed installs. lazySeedPending marks the
	// one loadEntries call whose buildIndex must skip the consume; a later
	// buildIndex (fork, session switch) cancels the in-flight seed and consumes
	// synchronously instead. cacheSeedGen invalidates an in-flight seed when
	// the scan state is reset underneath it.
	lazySeedPending  bool
	cacheScanSeeding bool
	cacheScanPending []*SessionEntry
	cacheSeedGen     int
	cacheSeedDone    chan struct{}
	byID             map[string]*SessionEntry
	labelsByID       map[string]string
	labelTimestamps  map[string]string
	leafID           *string

	// branchCache memoizes the current leaf's root path. GetBranch("") is
	// called on every footer paint (context usage) and rebuilding a long
	// session's branch is O(entries). The key is (leaf, entry count); the
	// result is shared read-only.
	// projection is the cached session projection for the branch above; see
	// session_projection.go.
	projection projectionCache
	// messages memoizes each entry's decoded messages and role, so a projection
	// after an append only decodes the appended entries; see
	// session_messagecache.go.
	messages messageCache
	// cacheScan is the running cache-miss scan state, so the cache-miss notice
	// for an assistant message does not rescan the session; see cachestats.go.
	cacheScan cacheScanState

	// branchCache holds the current leaf's path twice: pointers for the
	// internal readers (Projection, ContextSignature), which never copy the
	// path, and values for callers that hand it out (GetBranch, the footer).
	// Both are keyed by the branch version and built independently.
	branchCacheVersion   projectionVersion
	branchCachePtrs      []*SessionEntry
	branchCacheValues    []SessionEntry
	branchCacheHasValues bool
	branchCacheValid     bool
}

// SessionManagerOptions are the constructor inputs.
type SessionManagerOptions struct {
	// SessionDir is the directory for session files.
	SessionDir string
	// Persist enables file persistence. Default true.
	Persist *bool
	// WriteQueue, when non-nil, moves session file writes off the calling
	// goroutine through the internal/offloop uniform mechanism: the line is
	// marshaled synchronously (the caller's lock stays authoritative) and the
	// file write runs on the queue's worker in submission order. Interactive
	// mode wires one because its callers include the UI loop, which must never
	// block; nil (the default) keeps writes synchronous for one-shot
	// consumers. A manager with a queue must have FlushWrites called before
	// process exit.
	WriteQueue *offloop.Queue
}

// NewSessionManager creates a session manager with a fresh session
// (SessionManager.create / inMemory).
func NewSessionManager(cwd string, options *SessionManagerOptions) *SessionManager {
	persist := true
	if options != nil && options.Persist != nil {
		persist = *options.Persist
	}
	sessionDir := ""
	if options != nil {
		sessionDir = options.SessionDir
	}
	m := &SessionManager{
		cwd:             ResolvePath(cwd, "", PathInputOptions{}),
		sessionDir:      NormalizePath(sessionDir, PathInputOptions{}),
		persist:         persist,
		byID:            map[string]*SessionEntry{},
		labelsByID:      map[string]string{},
		labelTimestamps: map[string]string{},
	}
	if options != nil {
		m.writeQueue = options.WriteQueue
	}
	// Without an explicit directory, persisted sessions live under the agent
	// dir's per-cwd sessions path (upstream create -> getDefaultSessionDir);
	// in-memory sessions (persist=false) keep no directory.
	if persist && m.sessionDir == "" {
		m.sessionDir = DefaultSessionDir(cwd, "")
	}
	var directoryErr error
	if persist && m.sessionDir != "" {
		directoryErr = os.MkdirAll(m.sessionDir, 0o777)
	}
	m.NewSession(nil)
	if directoryErr != nil {
		m.recordWriteResult(m.sessionFile, "mkdir", directoryErr)
	}
	return m
}

// NewSessionOptions are NewSession inputs.
type NewSessionOptions struct {
	ID            string
	ParentSession *string
}

// NewSession starts a fresh session (v3 header).
func (m *SessionManager) NewSession(options *NewSessionOptions) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if options != nil && options.ID != "" {
		if err := AssertValidSessionID(options.ID); err != nil {
			panic(err)
		}
	}
	if options != nil && options.ID != "" {
		m.sessionID = options.ID
	} else {
		m.sessionID = UUIDv7()
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	header := SessionHeader{
		Version:   intPtr(CurrentSessionVersion),
		ID:        m.sessionID,
		Timestamp: timestamp,
		Cwd:       m.cwd,
	}
	if options != nil {
		header.ParentSession = options.ParentSession
	}
	m.fileEntries = []FileEntry{{Header: &header}}
	m.byID = map[string]*SessionEntry{}
	m.labelsByID = map[string]string{}
	m.labelTimestamps = map[string]string{}
	m.leafID = nil
	m.messages.reset()
	m.cacheScan.reset()
	m.cacheSeedGen++
	m.cacheScanSeeding = false
	m.cacheScanPending = nil
	m.writeStarted = false
	m.writeState = &sessionWriteState{}

	if m.persist {
		fileTimestamp := strings.NewReplacer(":", "-", ".", "-").Replace(timestamp)
		m.sessionFile = filepath.Join(m.SessionDir(), fmt.Sprintf("%s_%s.jsonl", fileTimestamp, m.sessionID))
	}
	return m.sessionFile
}

func intPtr(n int) *int { return &n }

// OpenSession loads a session file (SessionManager.open).
func OpenSession(path string, sessionDir string, cwdOverride string) (*SessionManager, error) {
	resolvedPath := ResolvePath(path, "", PathInputOptions{})
	m := &SessionManager{
		persist:         true,
		writeState:      &sessionWriteState{initialized: true, claimed: true},
		byID:            map[string]*SessionEntry{},
		labelsByID:      map[string]string{},
		labelTimestamps: map[string]string{},
	}
	entries, _, _, err := LoadEntriesFromFileBuffered(resolvedPath)
	if err != nil {
		return nil, err
	}
	cwd := cwdOverride
	if cwd == "" {
		if len(entries) > 0 && entries[0].Header != nil {
			cwd = entries[0].Header.Cwd
		}
		if cwd == "" {
			if current, err := os.Getwd(); err == nil {
				cwd = CanonicalizePath(current)
			}
		}
	}
	m.cwd = ResolvePath(cwd, "", PathInputOptions{})
	if sessionDir != "" {
		m.sessionDir = NormalizePath(sessionDir, PathInputOptions{})
	} else {
		m.sessionDir = filepath.Dir(resolvedPath)
	}

	if fileExists(resolvedPath) {
		if len(entries) == 0 {
			info, _ := os.Stat(resolvedPath)
			if info.Size() > 0 {
				return nil, fmt.Errorf("Session file is not a valid pi session: %s", resolvedPath)
			}
			m.NewSession(nil)
			m.sessionFile = resolvedPath
			m.rewriteFile()
			m.writeStarted = true
			return m, nil
		}
		m.sessionFile = resolvedPath
		m.lazySeedPending = true
		m.loadEntries(entries, nil)
		m.seedCacheScanInBackground()
		m.writeStarted = true
	} else {
		m.NewSession(nil)
		m.sessionFile = resolvedPath
	}
	return m, nil
}

// ContinueRecentSession continues the most recent session or creates one
// (SessionManager.continueRecent). Like upstream, a session that cannot be
// opened is an error: silently starting a new one would look like lost history.
func ContinueRecentSession(cwd string, sessionDir string) (*SessionManager, error) {
	dir := sessionDir
	if dir == "" {
		dir = DefaultSessionDir(cwd, "")
	}
	filterCwd := sessionDir != "" && dir != DefaultSessionDir(cwd, "")
	filterValue := ""
	if filterCwd {
		filterValue = cwd
	}
	if mostRecent := FindMostRecentSession(dir, filterValue); mostRecent != nil {
		return OpenSession(*mostRecent, dir, cwd)
	}
	return NewSessionManager(cwd, &SessionManagerOptions{SessionDir: dir}), nil
}

// InMemorySession builds a non-persisting session, optionally from entries
// (SessionManager.inMemory).
func InMemorySession(cwd string, options *NewSessionOptions, entries []FileEntry) *SessionManager {
	m := NewSessionManager(cwd, &SessionManagerOptions{Persist: boolPtr(false)})
	if len(entries) > 0 {
		m.loadEntries(entries, options)
	}
	return m
}

// loadEntries initializes from file entries (port of _loadEntries).
func (m *SessionManager) loadEntries(entries []FileEntry, options *NewSessionOptions) {
	var header *SessionHeader
	for _, entry := range entries {
		if entry.Header != nil {
			header = entry.Header
			break
		}
	}
	if header != nil {
		m.fileEntries = entries
		m.sessionID = header.ID
		if migrateToCurrentVersion(m.fileEntries) {
			m.rewriteFile()
		}
	} else {
		m.NewSession(options)
		m.fileEntries = append(m.fileEntries, entries...)
	}
	m.buildIndex()
}

// seedCacheScanInBackground consumes the lazily loaded entries' cache-scan
// facts off the UI goroutine: the scan parses every message's JSON, ~250ms on
// a 55MB session, and the load path must not pay it. Until the seed installs,
// cache statistics are empty and appends queue in order; the seed installs
// under the manager lock and drains the queue, so the combined state matches
// an eager consume exactly.
func (m *SessionManager) seedCacheScanInBackground() {
	m.cacheScanSeeding = true
	m.cacheSeedDone = make(chan struct{})
	gen := m.cacheSeedGen
	shells := make([]FileEntry, len(m.fileEntries))
	copy(shells, m.fileEntries)
	go func() {
		var state cacheScanState
		for i := range shells {
			if entry := shells[i].Entry; entry != nil {
				state.consume(entry)
			}
		}
		m.mu.Lock()
		if m.cacheSeedGen != gen {
			m.mu.Unlock()
			return
		}
		m.cacheScan.prev = state.prev
		m.cacheScan.candidates = state.candidates
		m.cacheScan.entryIndex = state.entryIndex
		m.cacheScanSeeding = false
		pending := m.cacheScanPending
		m.cacheScanPending = nil
		for _, entry := range pending {
			m.cacheScan.consume(entry)
		}
		m.mu.Unlock()
		close(m.cacheSeedDone)
	}()
}

func (m *SessionManager) buildIndex() {
	lazy := m.lazySeedPending
	m.lazySeedPending = false
	if !lazy && m.cacheScanSeeding {
		// A rebuild while a seed is in flight (fork, session switch) replaces
		// the entry list the seed was walking: cancel it and consume this
		// list synchronously. Queued appends belong to the replaced list.
		m.cacheSeedGen++
		m.cacheScanSeeding = false
		m.cacheScanPending = nil
	}
	m.byID = map[string]*SessionEntry{}
	m.labelsByID = map[string]string{}
	m.labelTimestamps = map[string]string{}
	m.leafID = nil
	m.branchCacheValid = false
	// Re-seed the cache-miss scan state over the loaded entries, in file order.
	m.cacheScan.reset()
	for i := range m.fileEntries {
		entry := m.fileEntries[i].Entry
		if entry == nil {
			continue
		}
		if !lazy {
			m.cacheScan.consume(entry)
		}
		m.byID[entry.ID] = entry
		leafID := entry.ID
		m.leafID = &leafID
		if entry.Type == "label" && entry.TargetID != "" {
			if entry.Label != nil && *entry.Label != "" {
				m.labelsByID[entry.TargetID] = *entry.Label
				m.labelTimestamps[entry.TargetID] = entry.Timestamp
			} else {
				delete(m.labelsByID, entry.TargetID)
				delete(m.labelTimestamps, entry.TargetID)
			}
		}
	}
}

func (m *SessionManager) rewriteFile() {
	if !m.persist || m.sessionFile == "" {
		return
	}
	path, state := m.sessionFile, m.writeState
	if m.writeQueue == nil {
		m.recordWriteResult(path, "rewrite", state.rewrite(path, m.fileEntries))
		return
	}
	// Snapshot under the manager lock; the worker serializes and writes in
	// FIFO order and touches only its captured write state and diagnostics.
	snapshot := append([]FileEntry(nil), m.fileEntries...)
	m.writeQueue.Go(func() {
		m.recordWriteResult(path, "rewrite", state.rewrite(path, snapshot))
	})
}

// GetWriteQueue returns the wired write queue, or nil for synchronous writes.
func (m *SessionManager) GetWriteQueue() *offloop.Queue { return m.writeQueue }

// SetWriteQueue wires (or replaces) the write queue; used by the interactive
// wiring for managers it opens ad hoc (session rename, model list).
func (m *SessionManager) SetWriteQueue(queue *offloop.Queue) { m.writeQueue = queue }

// IsPersisted reports file persistence.
func (m *SessionManager) IsPersisted() bool { return m.persist }

// GetCwd returns the session working directory.
func (m *SessionManager) GetCwd() string { return m.cwd }

// GetSessionDir returns the session directory.
func (m *SessionManager) GetSessionDir() string { return m.sessionDir }

// GetSessionID returns the session id.
func (m *SessionManager) GetSessionID() string { return m.sessionID }

// GetSessionFile returns the session file path ("" when unset).
func (m *SessionManager) GetSessionFile() string { return m.sessionFile }

// hasConversation reports whether the session contains a user or assistant
// message. Setup entries alone (model, thinking level, system prompt) stay in
// memory so opening and closing without chatting leaves no file, while starting
// at the user message keeps the prompt on disk if the first turn never
// completes (upstream _hasConversation, ff72faba2).
func (m *SessionManager) hasConversation() bool {
	for i := range m.fileEntries {
		e := m.fileEntries[i].Entry
		if e == nil || e.Type != "message" {
			continue
		}
		var msg struct {
			Role string `json:"role"`
		}
		if json.Unmarshal(e.Message, &msg) == nil && (msg.Role == "user" || msg.Role == "assistant") {
			return true
		}
	}
	return false
}

// persistEntry writes the entry, honoring the flush-on-first-conversation
// contract: the file is only created once a user or assistant message exists
// (upstream _persist).
func (m *SessionManager) persistEntry(entry *SessionEntry) {
	if !m.persist || m.sessionFile == "" {
		return
	}
	if !m.writeStarted {
		if !m.hasConversation() {
			return
		}
		m.rewriteFile()
		m.writeStarted = true
		return
	}
	m.appendLine(entry)
}

func (m *SessionManager) appendLine(entry *SessionEntry) {
	line, marshalErr := MarshalFileEntry(FileEntry{Entry: entry})
	path, state := m.sessionFile, m.writeState
	// File-entry slots are append-only. This immutable prefix view costs O(1)
	// and lets a failed append recover all accepted entries on the next save.
	// New sessions/branches replace the slice and write state, so an old worker
	// cannot apply its completion to a replacement session.
	snapshot := m.fileEntries[:len(m.fileEntries):len(m.fileEntries)]
	write := func() { m.recordWriteResult(path, "append", state.append(path, line, marshalErr, snapshot)) }
	if m.writeQueue == nil {
		write()
		return
	}
	m.writeQueue.Go(write)
}

func (m *SessionManager) appendEntry(entry *SessionEntry) string {
	m.fileEntries = append(m.fileEntries, FileEntry{Entry: entry})
	m.byID[entry.ID] = entry
	leafID := entry.ID
	m.leafID = &leafID
	m.cacheScan.consume(entry)
	m.persistEntry(entry)
	return entry.ID
}

// nextEntry builds an entry child of the current leaf.
func (m *SessionManager) nextEntry(entryType string) SessionEntry {
	return SessionEntry{
		Type: entryType,
		SessionEntryBase: SessionEntryBase{
			ID:        generateID(m.byID),
			ParentID:  m.leafID,
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
}

// AppendMessage appends a message as a child of the current leaf.
func (m *SessionManager) AppendMessage(message ai.Message) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	encoded, err := ai.MarshalMessage(message)
	if err != nil {
		panic(err)
	}
	entry := m.nextEntry("message")
	entry.Message = encoded
	return m.appendEntry(&entry)
}

// AppendThinkingLevelChange appends a thinking level change.
func (m *SessionManager) AppendThinkingLevelChange(thinkingLevel string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.nextEntry("thinking_level_change")
	entry.ThinkingLevel = thinkingLevel
	return m.appendEntry(&entry)
}

// AppendModelChange appends a model change.
func (m *SessionManager) AppendModelChange(provider, modelID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.nextEntry("model_change")
	entry.Provider = provider
	entry.ModelID = modelID
	return m.appendEntry(&entry)
}

// AppendCompaction appends a compaction summary with the prompt/tool state
// captured at the boundary.
func (m *SessionManager) AppendCompaction(summary string, firstKeptEntryID string, tokensBefore int64, details json.RawMessage, fromHook bool, usage *ai.Usage) string {
	// Resolve the carried system message from a projection taken outside the
	// lock: projecting a large session costs hundreds of milliseconds, and
	// holding m.mu across it parks every other reader (the footer's per-frame
	// session reads) for that long. Upstream is single-threaded and pays the
	// same projection, which its parsed-object entries make cheap.
	systemMessage := m.CurrentSystemMessage()
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	timestamp := now.Format(time.RFC3339Nano)
	entry := m.nextEntry("compaction")
	entry.Timestamp = timestamp
	entry.Summary = summary
	// A compaction with no kept entry points at itself (upstream
	// `firstKeptEntryId: firstKeptEntryId ?? id`), so the window keeps the
	// summary instead of dropping everything before it.
	entry.FirstKeptEntryID = firstKeptEntryID
	if entry.FirstKeptEntryID == "" {
		entry.FirstKeptEntryID = entry.ID
	}
	entry.TokensBefore = tokensBefore
	entry.Details = details
	entry.Usage = usage
	if fromHook {
		entry.FromHook = boolPtr(true)
	}
	if systemMessage != nil {
		// The recorded system message carries the compaction's own timestamp
		// (upstream stamps it with new Date(timestamp).getTime()).
		systemMessage.Timestamp = now.UnixMilli()
		entry.SystemMessageJSON = mustMarshalJSON(systemMessage)
	}
	return m.appendEntry(&entry)
}

// AppendCustomEntry appends an extension state entry (no LLM context).
func (m *SessionManager) AppendCustomEntry(customType string, data json.RawMessage) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.nextEntry("custom")
	entry.CustomType = customType
	entry.Data = data
	return m.appendEntry(&entry)
}

// AppendSessionInfo appends a display-name entry.
func (m *SessionManager) AppendSessionInfo(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	sanitizedName := ai.JSTrim(newlinePattern.ReplaceAllString(name, " "))
	entry := m.nextEntry("session_info")
	entry.Name = &sanitizedName
	return m.appendEntry(&entry)
}

var newlinePattern = regexp.MustCompile(`[\r\n]+`)

// GetSessionName resolves the latest session_info name (empty clears).
func (m *SessionManager) GetSessionName() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Upstream's getSessionName reads fileEntries directly for the same reason,
	// in its own words: "the footer calls this on every frame, and getEntries()
	// copies the whole session". The program-status reporter calls this on every
	// status event while a run is active, so getEntriesLocked()'s copy of every
	// entry (440 B each, ~84 MB at 40k entries) under m.mu was stalling the UI
	// loop: 80 of 134 event-apply stall records caught it inside that copy, and
	// 20 more waited for the lock in CacheMissFor.
	for i := len(m.fileEntries) - 1; i >= 0; i-- {
		entry := m.fileEntries[i].Entry
		if entry == nil || entry.Type != "session_info" {
			continue
		}
		// Upstream returns at the newest session_info entry even when its name
		// is missing (`entry.name?.trim() || undefined`).
		if entry.Name == nil {
			return ""
		}
		name := ai.JSTrim(*entry.Name)
		if name != "" {
			return name
		}
		return ""
	}
	return ""
}

// AppendCustomMessageEntry appends an LLM-context custom message entry.
func (m *SessionManager) AppendCustomMessageEntry(customType string, content json.RawMessage, display bool, details json.RawMessage) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.nextEntry("custom_message")
	entry.CustomType = customType
	entry.Content = content
	entry.Display = &display
	entry.Details = details
	return m.appendEntry(&entry)
}

// GetLeafID returns the current leaf (nil before any entries).
func (m *SessionManager) GetLeafID() *string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.leafID
}

// GetEntry returns one entry by id.
func (m *SessionManager) GetEntry(id string) *SessionEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byID[id]
}

// GetLabel returns an entry's label.
func (m *SessionManager) GetLabel(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.labelsByID[id]
}

// AppendContextEdit appends a context_edit entry for targetID (upstream
// appendContextEdit): a null replacement omits the target from model context, a
// {"content": ...} replacement replaces only its content. Assistant and
// tool-result string replacements are stored wrapped as a text block.
func (m *SessionManager) AppendContextEdit(targetID string, replacement json.RawMessage) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	trimmed := bytes.TrimSpace(replacement)
	isNull := len(replacement) == 0 || bytes.Equal(trimmed, []byte("null"))
	var content json.RawMessage
	if !isNull {
		var holder struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(replacement, &holder); err != nil {
			return "", fmt.Errorf("Context edit replacement must be null or contain string/array content")
		}
		content = bytes.TrimSpace(holder.Content)
		if len(content) == 0 || (content[0] != '"' && content[0] != '[') {
			return "", fmt.Errorf("Context edit replacement must be null or contain string/array content")
		}
	}
	target, ok := m.byID[targetID]
	if !ok {
		return "", fmt.Errorf("Entry %s not found", targetID)
	}
	onBranch := false
	for _, entry := range m.branchPointersLocked("") {
		if entry.ID == targetID {
			onBranch = true
			break
		}
	}
	if !onBranch {
		return "", fmt.Errorf("Entry %s is not on the active branch", targetID)
	}
	role := ""
	editable := target.Type == "custom_message"
	if target.Type == "message" {
		var probe struct {
			Role string `json:"role"`
		}
		_ = json.Unmarshal(target.Message, &probe)
		role = probe.Role
		editable = role == string(ai.RoleUser) || role == string(ai.RoleAssistant) || role == string(ai.RoleToolResult)
	}
	if !editable {
		return "", fmt.Errorf("Entry %s does not contribute editable model content", targetID)
	}
	if !isNull && (role == string(ai.RoleAssistant) || role == string(ai.RoleToolResult)) && content[0] == '"' {
		var text string
		if err := json.Unmarshal(content, &text); err == nil {
			if encoded, err := ai.MarshalJSON(ai.ContentList{ai.TextContent{Text: text}}); err == nil {
				if normalized, err := ai.MarshalJSON(map[string]json.RawMessage{"content": encoded}); err == nil {
					replacement = normalized
				}
			}
		}
	}
	entry := m.nextEntry("context_edit")
	entry.TargetID = targetID
	entry.Replacement = replacement
	return m.appendEntry(&entry), nil
}

// AppendLabelChange sets or clears a label bookmark.
func (m *SessionManager) AppendLabelChange(targetID string, label *string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[targetID]; !ok {
		return "", fmt.Errorf("Entry %s not found", targetID)
	}
	entry := m.nextEntry("label")
	entry.TargetID = targetID
	entry.Label = label
	id := m.appendEntry(&entry)
	if label != nil && *label != "" {
		m.labelsByID[targetID] = *label
		m.labelTimestamps[targetID] = entry.Timestamp
	} else {
		delete(m.labelsByID, targetID)
		delete(m.labelTimestamps, targetID)
	}
	return id, nil
}

// GetBranch walks from an entry (or the leaf) to the root. The result is
// shared and must be treated as read-only; the current leaf's path is cached
// and only rebuilt when the leaf or the entry set changes.
func (m *SessionManager) GetBranch(fromID string) []SessionEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.branchLocked(fromID)
}

// branchLocked is GetBranch for callers already holding m.mu. The current
// leaf's value path is cached as well, because callers hand it out.
func (m *SessionManager) branchLocked(fromID string) []SessionEntry {
	if fromID == "" {
		if m.branchCacheValid && m.branchCacheHasValues && m.branchCacheVersion == m.projectionVersionLocked() {
			return m.branchCacheValues
		}
		values := materializeEntries(m.branchPointersLocked(""))
		// Drop the spare capacity so a caller's append cannot write into the
		// shared cache's backing array.
		values = values[:len(values):len(values)]
		m.branchCacheValues = values
		m.branchCacheHasValues = true
		return values
	}
	return materializeEntries(m.branchPointersLocked(fromID))
}

// branchPointersLocked walks from an entry (or the leaf) to the root and
// returns pointers into the manager's canonical entries. The leaf's path is
// cached by branch version; the result is shared read-only, and entries are
// immutable after they are appended, so a pointer stays valid for as long as
// the caller needs the path.
func (m *SessionManager) branchPointersLocked(fromID string) []*SessionEntry {
	startID := fromID
	if startID == "" {
		if m.leafID == nil {
			return nil
		}
		startID = *m.leafID
		if m.branchCacheValid && m.branchCacheVersion == m.projectionVersionLocked() {
			return m.branchCachePtrs
		}
	}
	var path []*SessionEntry
	current := m.byID[startID]
	for current != nil {
		path = append(path, current)
		if current.ParentID != nil {
			current = m.byID[*current.ParentID]
		} else {
			current = nil
		}
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	if fromID == "" {
		// Drop the spare capacity so a caller's append cannot write into the
		// shared cache's backing array.
		path = path[:len(path):len(path)]
		m.branchCachePtrs = path
		m.branchCacheVersion = m.projectionVersionLocked()
		m.branchCacheValid = true
		m.branchCacheHasValues = false
	}
	return path
}

// SetMessageDecodeForTest replaces the entry-to-messages step of the session's
// message memo (test seam, like WaitForPendingLoads). Tests use it to make a
// scan's decode observable, e.g. to prove the scan is not running on the UI
// loop.
func (m *SessionManager) SetMessageDecodeForTest(decode func(*SessionEntry) []ai.Message) {
	m.messages.mu.Lock()
	m.messages.decode = decode
	m.messages.mu.Unlock()
}

// BuildContextEntriesForLeaf builds the active compaction-aware entry list
// from the current leaf.
func (m *SessionManager) BuildContextEntriesForLeaf() []SessionEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buildContextEntriesForLeafLocked()
}

// buildContextEntriesForLeafLocked is BuildContextEntriesForLeaf for callers
// already holding m.mu.
func (m *SessionManager) buildContextEntriesForLeafLocked() []SessionEntry {
	return compactionWindowFromPointers(m.branchPointersLocked(""), &m.messages)
}

// GetHeader returns the session header.
func (m *SessionManager) GetHeader() *SessionHeader {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range m.fileEntries {
		if entry.Header != nil {
			return entry.Header
		}
	}
	return nil
}

// GetEntries returns all entries (excluding the header).
func (m *SessionManager) GetEntries() []SessionEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getEntriesLocked()
}

// Branch starts a new branch from an earlier entry.
func (m *SessionManager) Branch(branchFromID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[branchFromID]; !ok {
		return fmt.Errorf("Entry %s not found", branchFromID)
	}
	leafID := branchFromID
	m.leafID = &leafID
	return nil
}

// ResetLeaf clears the leaf pointer (next append creates a new root).
func (m *SessionManager) ResetLeaf() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leafID = nil
}

// BranchWithSummary starts a branch with a summary of the abandoned path.
func (m *SessionManager) BranchWithSummary(branchFromID string, summary string, details json.RawMessage, fromHook bool, usage *ai.Usage) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if branchFromID != "" {
		if _, ok := m.byID[branchFromID]; !ok {
			return ""
		}
	}
	fromID := "root"
	if m.leafID != nil {
		fromID = *m.leafID
	}
	m.leafID = &branchFromID
	entry := m.nextEntry("branch_summary")
	entry.ParentID = &branchFromID
	entry.FromID = fromID
	entry.Summary = summary
	entry.Details = details
	entry.Usage = usage
	if fromHook {
		entry.FromHook = boolPtr(true)
	}
	return m.appendEntry(&entry)
}

// CreateBranchedSession writes a new session file holding only the path to
// the given leaf (simplified port: label re-chaining included).
func (m *SessionManager) CreateBranchedSession(leafID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// branchLocked, not GetBranch: the manager lock is already held (the
	// re-entrant GetBranch deadlocked every persisted fork/clone).
	path := m.branchLocked(leafID)
	if len(path) == 0 {
		return "", fmt.Errorf("Entry %s not found", leafID)
	}

	newSessionID := UUIDv7()
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	fileTimestamp := strings.NewReplacer(":", "-", ".", "-").Replace(timestamp)
	newSessionFile := filepath.Join(m.sessionDir, fmt.Sprintf("%s_%s.jsonl", fileTimestamp, newSessionID))

	header := SessionHeader{
		Version:   intPtr(CurrentSessionVersion),
		ID:        newSessionID,
		Timestamp: timestamp,
		Cwd:       m.cwd,
	}
	if m.persist {
		header.ParentSession = &m.sessionFile
	}

	// Re-chain the path (dropping label entries) with fresh parent ids.
	var newEntries []FileEntry
	replacementByLabelID := map[string]string{}
	pathParentID := ""
	for i := range path {
		entry := path[i]
		if entry.Type == "label" {
			replacementByLabelID[entry.ID] = "" // resolved lazily by consumers
			continue
		}
		if entry.Type == "compaction" && entry.FirstKeptEntryID != "" {
			if replacement, ok := replacementByLabelID[entry.FirstKeptEntryID]; ok && replacement != "" {
				entry.setFirstKeptEntryID(replacement)
			}
		}
		if pathParentID == "" {
			entry.setParentID(nil)
		} else {
			parent := pathParentID
			entry.setParentID(&parent)
		}
		pathParentID = entry.ID
		newEntries = append(newEntries, FileEntry{Entry: &entry})
	}

	// Recreate resolved labels for retained targets, including labels recorded
	// outside the selected path. Slots preserve Map insertion order: updating a
	// label keeps its position, while deleting and re-adding moves it to the end.
	retained := make(map[string]*SessionEntry, len(newEntries))
	for _, entry := range newEntries {
		retained[entry.Entry.ID] = entry.Entry
	}
	labelSlots := map[string]int{}
	var labels []*SessionEntry
	for _, fileEntry := range m.fileEntries {
		entry := fileEntry.Entry
		if entry == nil || entry.Type != "label" || retained[entry.TargetID] == nil {
			continue
		}
		slot, exists := labelSlots[entry.TargetID]
		if entry.Label == nil || *entry.Label == "" {
			if exists {
				labels[slot] = nil
				delete(labelSlots, entry.TargetID)
			}
		} else if exists {
			labels[slot] = entry
		} else {
			labelSlots[entry.TargetID] = len(labels)
			labels = append(labels, entry)
		}
	}
	for _, label := range labels {
		if label == nil {
			continue
		}
		parent := pathParentID
		entry := &SessionEntry{
			Type: "label",
			SessionEntryBase: SessionEntryBase{
				ID: generateID(retained), ParentID: &parent, Timestamp: label.Timestamp,
			},
			TargetID: label.TargetID, Label: label.Label,
		}
		retained[entry.ID] = entry
		pathParentID = entry.ID
		newEntries = append(newEntries, FileEntry{Entry: entry})
	}

	m.fileEntries = append([]FileEntry{{Header: &header}}, newEntries...)
	m.sessionID = newSessionID
	m.writeState = &sessionWriteState{}
	// In-memory sessions branch with no file, as upstream does.
	if m.persist {
		m.sessionFile = newSessionFile
	}
	m.buildIndex()

	// Use the same rule as persistEntry: write now if the branched path already
	// has a conversation, otherwise let persistEntry create the file later.
	if m.persist {
		if m.hasConversation() {
			m.rewriteFile()
			m.writeStarted = true
		} else {
			m.writeStarted = false
		}
		return newSessionFile, nil
	}
	return "", nil
}

// SessionInfo is the listing info for one session file.
type SessionInfo struct {
	Path              string
	ID                string
	Cwd               string
	Name              string
	ParentSessionPath string
	Created           time.Time
	Modified          time.Time
	MessageCount      int
	FirstMessage      string
	AllMessagesText   string
}

// ListSessions lists sessions for a cwd's directory, newest first
// (port of SessionManager.list; progress callback omitted).
func ListSessions(cwd string, sessionDir string) []SessionInfo {
	dir := sessionDir
	if dir == "" {
		dir = DefaultSessionDir(cwd, "")
	}
	filterCwd := sessionDir != "" && dir != DefaultSessionDir(cwd, "")
	resolvedCwd := ResolvePath(cwd, "", PathInputOptions{})

	var sessions []SessionInfo
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return sessions
	}
	for _, dirEntry := range dirEntries {
		name := DecodeUTF8WithReplacement([]byte(dirEntry.Name()))
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		full := filepath.Join(dir, name)
		if info := buildSessionInfo(full); info != nil {
			if filterCwd && ResolvePath(info.Cwd, "", PathInputOptions{}) != resolvedCwd {
				continue
			}
			sessions = append(sessions, *info)
		}
	}
	return sortSessionInfos(sessions)
}

// buildSessionInfo extracts listing metadata (port of buildSessionInfo). The
// file is streamed line by line like upstream's createReadStream + readline
// loop; reading it whole and splitting it doubled the memory of listing a
// large session directory.
func buildSessionInfo(filePath string) *SessionInfo {
	stats, err := os.Stat(filePath)
	if err != nil {
		return nil
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64*1024)

	var header *SessionHeader
	messageCount := 0
	firstMessage := ""
	var allMessages []string
	name := ""
	var lastActivityTime int64

	// recordActivity applies upstream getMessageActivityTime: only a user or
	// assistant message that carries a content member counts, using its numeric
	// timestamp when present and the entry timestamp otherwise.
	recordActivity := func(role string, hasContent bool, messageTimestamp int64, hasMessageTimestamp bool, entryTimestamp int64) {
		if (role != "user" && role != "assistant") || !hasContent {
			return
		}
		timestamp := entryTimestamp
		if hasMessageTimestamp {
			timestamp = messageTimestamp
		}
		if timestamp > lastActivityTime {
			lastActivityTime = timestamp
		}
	}
	recordText := func(role string, text string) {
		if (role != "user" && role != "assistant") || text == "" {
			return
		}
		allMessages = append(allMessages, text)
		if firstMessage == "" && role == "user" {
			firstMessage = text
		}
	}
	// recordMessageLine records activity from the raw message object and text
	// from the decoded message (the shared path when the scanner declines).
	recordMessageLine := func(rawMessage []byte, entryTimestamp int64) {
		if activity, ok := scanMessageActivity(rawMessage); ok {
			recordActivity(activity.role, activity.hasContent, activity.timestamp, activity.hasTimestamp, entryTimestamp)
		}
		if message, err := ai.UnmarshalMessage(json.RawMessage(rawMessage)); err == nil {
			recordText(string(ai.RoleOf(message)), extractTextContent(message))
		}
	}

	// process consumes one JSONL line. It reports true when the line is a
	// parsable entry that is not a session header: the file is then not a
	// session (upstream returns null from the same check).
	process := func(line []byte) bool {
		if header == nil {
			entry, err := UnmarshalFileEntry(string(line))
			if err != nil {
				return false
			}
			if entry.Header == nil {
				return true
			}
			header = entry.Header
			return false
		}
		// Message entries dominate a session's bytes; the escape-free scanner
		// the lazy loader uses avoids a full structural decode per line.
		// Anything it does not fully understand falls back to the decoder.
		if meta, ok := scanSessionLine(line); ok && meta.typ == "message" && meta.id != "" {
			messageCount++
			entryTimestamp := entryTimestampMillis(meta.timestamp)
			if listing := scanListingMessage(meta.message); listing.ok {
				recordActivity(listing.role, listing.hasContent, listing.timestamp, listing.hasTimestamp, entryTimestamp)
				recordText(listing.role, listing.text)
			} else {
				recordMessageLine(meta.message, entryTimestamp)
			}
			return false
		}
		entry, err := UnmarshalFileEntry(string(line))
		if err != nil || entry.Entry == nil {
			return false
		}
		e := entry.Entry
		if e.Type == "session_info" && e.Name != nil {
			name = strings.TrimSpace(*e.Name)
		}
		if e.Type == "message" {
			messageCount++
			recordMessageLine(e.Message, entryTimestampMillis(e.Timestamp))
		}
		return false
	}

	for {
		line, readErr := reader.ReadBytes('\n')
		line = bytes.TrimRight(line, "\r\n")
		if len(bytes.TrimSpace(line)) != 0 && process(line) {
			return nil
		}
		if readErr != nil {
			break
		}
	}
	if header == nil {
		return nil
	}

	modified := stats.ModTime()
	if lastActivityTime > 0 {
		modified = time.UnixMilli(lastActivityTime)
	} else if header.Timestamp != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, header.Timestamp); err == nil {
			modified = parsed
		}
	}
	created := stats.ModTime()
	if header.Timestamp != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, header.Timestamp); err == nil {
			created = parsed
		}
	}

	cwd := header.Cwd
	first := firstMessage
	if first == "" {
		first = "(no messages)"
	}
	return &SessionInfo{
		Path: filePath, ID: header.ID, Cwd: cwd, Name: name,
		ParentSessionPath: derefOrEmpty(header.ParentSession),
		Created:           created, Modified: modified,
		MessageCount:    messageCount,
		FirstMessage:    first,
		AllMessagesText: strings.Join(allMessages, " "),
	}
}

// entryTimestampMillis parses an entry timestamp like the reference's
// Date.parse, returning 0 (ignored by the activity comparison) when it is not
// a valid RFC 3339 value.
func entryTimestampMillis(value string) int64 {
	if value == "" {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}
	return parsed.UnixMilli()
}

// messageActivity is the raw-message shape upstream getMessageActivityTime
// reads: a string role, a present content member, and a numeric timestamp.
type messageActivity struct {
	role         string
	hasContent   bool
	timestamp    int64
	hasTimestamp bool
}

// scanMessageActivity reads the activity fields from a raw message object,
// which keeps the "content" presence and the timestamp type that the typed
// message cannot express.
func scanMessageActivity(rawMessage []byte) (messageActivity, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawMessage, &fields); err != nil || fields == nil {
		return messageActivity{}, false
	}
	var activity messageActivity
	if role, ok := fields["role"]; ok {
		_ = json.Unmarshal(role, &activity.role)
	}
	_, activity.hasContent = fields["content"]
	if timestamp, ok := fields["timestamp"]; ok {
		var number float64
		if json.Unmarshal(timestamp, &number) == nil {
			activity.timestamp = int64(number)
			activity.hasTimestamp = true
		}
	}
	return activity, true
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// extractTextContent joins text blocks with spaces (message-content only).
func extractTextContent(message ai.Message) string {
	switch m := message.(type) {
	case *ai.SystemMessage:
		return m.Content.Text
	case *ai.UserMessage:
		return contentTextJoined(m.Content)
	case *ai.AssistantMessage:
		var texts []string
		for _, block := range m.Content {
			if tc, ok := block.(ai.TextContent); ok {
				texts = append(texts, tc.Text)
			}
		}
		return strings.Join(texts, " ")
	case *ai.ToolResultMessage:
		var texts []string
		for _, block := range m.Content {
			if tc, ok := block.(ai.TextContent); ok {
				texts = append(texts, tc.Text)
			}
		}
		return strings.Join(texts, " ")
	}
	return ""
}

func contentTextJoined(content ai.StringOrBlocks) string {
	if content.Blocks == nil {
		return content.Text
	}
	var texts []string
	for _, block := range content.Blocks {
		if tc, ok := block.(ai.TextContent); ok {
			texts = append(texts, tc.Text)
		}
	}
	return strings.Join(texts, " ")
}

// SessionDir returns the session directory (exported accessor).
func (m *SessionManager) SessionDir() string { return m.sessionDir }

// SessionTreeNode is a node of the session entry tree (upstream getTree()).
type SessionTreeNode struct {
	Entry          SessionEntry
	Children       []*SessionTreeNode
	Label          string
	HasLabel       bool
	LabelTimestamp string
}

// GetTree builds the session entry tree with resolved labels.
func (m *SessionManager) GetTree() []*SessionTreeNode {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := m.getEntriesLocked()

	nodeMap := map[string]*SessionTreeNode{}
	var roots []*SessionTreeNode
	for _, entry := range entries {
		node := &SessionTreeNode{Entry: entry}
		if label, ok := m.labelsByID[entry.ID]; ok {
			node.Label = label
			node.HasLabel = true
			node.LabelTimestamp = m.labelTimestamps[entry.ID]
		}
		nodeMap[entry.ID] = node
	}

	for _, entry := range entries {
		node := nodeMap[entry.ID]
		if entry.ParentID == nil || *entry.ParentID == entry.ID {
			roots = append(roots, node)
			continue
		}
		if parent, ok := nodeMap[*entry.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			// Orphan: treat as root.
			roots = append(roots, node)
		}
	}

	// Sort children by timestamp (oldest first).
	stack := append([]*SessionTreeNode{}, roots...)
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		sort.SliceStable(node.Children, func(i int, j int) bool {
			left, leftErr := time.Parse(time.RFC3339Nano, node.Children[i].Entry.Timestamp)
			right, rightErr := time.Parse(time.RFC3339Nano, node.Children[j].Entry.Timestamp)
			// Upstream compares Date.getTime(), so offsets affect ordering and
			// sub-millisecond differences tie. Invalid dates compare as NaN,
			// which leaves their relative order unchanged in the stable sort.
			return leftErr == nil && rightErr == nil && left.UnixMilli() < right.UnixMilli()
		})
		stack = append(stack, node.Children...)
	}
	return roots
}

func (m *SessionManager) getEntriesLocked() []SessionEntry {
	var out []SessionEntry
	for _, entry := range m.fileEntries {
		if entry.Entry != nil {
			out = append(out, *entry.Entry)
		}
	}
	return out
}

// UsesDefaultSessionDir reports whether the session uses the default per-cwd
// directory.
func (m *SessionManager) UsesDefaultSessionDir() bool {
	return m.sessionDir == DefaultSessionDir(m.cwd, "")
}

// ListAllSessions lists sessions across every per-cwd directory (or one custom
// directory) sorted by modification time (port of SessionManager.listAll).
func ListAllSessions(sessionDir string) []SessionInfo {
	if sessionDir != "" {
		// Upstream listAll(customSessionDir) scans the directory without a cwd
		// filter (listSessionsFromDir), never through list().
		return sortSessionInfos(listSessionsFromDir(sessionDir))
	}
	sessionsDir := DefaultSessionsDir()
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil
	}
	var sessions []SessionInfo
	for _, entry := range entries {
		// Upstream keeps directories and symlinks.
		if !entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
			continue
		}
		name := DecodeUTF8WithReplacement([]byte(entry.Name()))
		sessions = append(sessions, listSessionsFromDir(filepath.Join(sessionsDir, name))...)
	}
	return sortSessionInfos(sessions)
}

// sortSessionInfos orders sessions like upstream sortSessionInfos: modified
// time descending, and for equal millisecond timestamps the filename
// descending (listAll's basename localeCompare tie-break, which for the
// timestamp_uuid session names is a byte comparison).
func sortSessionInfos(sessions []SessionInfo) []SessionInfo {
	sort.SliceStable(sessions, func(i, j int) bool {
		left, right := sessions[i].Modified.UnixMilli(), sessions[j].Modified.UnixMilli()
		if left != right {
			return left > right
		}
		return filepath.Base(sessions[i].Path) > filepath.Base(sessions[j].Path)
	})
	return sessions
}

// listSessionsFromDir scans one directory for session files without applying
// any cwd filter (upstream listSessionsFromDir).
func listSessionsFromDir(dir string) []SessionInfo {
	var sessions []SessionInfo
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return sessions
	}
	for _, dirEntry := range dirEntries {
		name := DecodeUTF8WithReplacement([]byte(dirEntry.Name()))
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		full := filepath.Join(dir, name)
		if info := buildSessionInfo(full); info != nil {
			sessions = append(sessions, *info)
		}
	}
	return sortSessionInfos(sessions)
}

// messageListing is the listing view of one message: the role, the message
// timestamp and the text the message contributes. Listing never reads tool
// arguments, thinking or tool-result bodies, so the scanner skips them instead
// of decoding them.
type messageListing struct {
	role         string
	timestamp    int64
	hasTimestamp bool
	hasContent   bool
	text         string
	ok           bool
}

// scanListingMessage extracts the listing view of a raw message object without
// materializing the blocks listing never reads. Text values are unescaped as
// they are read; everything else is skipped in place. Like scanSessionLine, the
// scanner only accepts shapes it fully understands — an unknown block type, a
// non-string field it needs, duplicate keys, trailing data — and reports
// ok=false so the caller full-decodes the message instead. The two paths can
// therefore differ only for sessions the full decoder rejects (D225).
func scanListingMessage(data []byte) messageListing {
	i := skipListingSpace(data, 0)
	if i >= len(data) || data[i] != '{' {
		return messageListing{}
	}
	i++
	var (
		listing                  messageListing
		sawRole, sawTimestamp    bool
		sawContent               bool
		contentStart, contentEnd int
		deferredContent          bool
	)
	first := true
	for {
		i = skipListingSpace(data, i)
		if i >= len(data) {
			return messageListing{}
		}
		if data[i] == '}' {
			i++
			break
		}
		if !first {
			if data[i] != ',' {
				return messageListing{}
			}
			i++
			i = skipListingSpace(data, i)
			if i < len(data) && data[i] == '}' {
				return messageListing{}
			}
		}
		key, n, ok := scanPlainStringBytes(data[i:])
		if !ok {
			return messageListing{}
		}
		i += n
		i = skipListingSpace(data, i)
		if i >= len(data) || data[i] != ':' {
			return messageListing{}
		}
		i++
		i = skipListingSpace(data, i)
		switch string(key) {
		case "role":
			if sawRole {
				return messageListing{}
			}
			sawRole = true
			value, n, ok := scanPlainStringBytes(data[i:])
			if !ok {
				return messageListing{}
			}
			listing.role = string(value)
			i += n
		case "timestamp":
			if sawTimestamp {
				return messageListing{}
			}
			sawTimestamp = true
			number, n, ok := scanListingInteger(data[i:])
			if !ok {
				return messageListing{}
			}
			listing.timestamp = number
			i += n
		case "content":
			if sawContent {
				return messageListing{}
			}
			sawContent = true
			if sawRole && (listing.role == "user" || listing.role == "assistant") {
				text, n, ok := scanListingContent(data[i:], listing.role)
				if !ok {
					return messageListing{}
				}
				listing.text = text
				i += n
				break
			}
			// Both key orders exist in stored messages, so content that
			// arrives before the role is remembered by range and decoded once
			// role says whether it is needed.
			n, ok := skipJSONValue(data[i:])
			if !ok {
				return messageListing{}
			}
			contentStart, contentEnd = i, i+n
			deferredContent = true
			i += n
		default:
			n, ok := skipJSONValue(data[i:])
			if !ok {
				return messageListing{}
			}
			i += n
		}
		first = false
	}
	i = skipListingSpace(data, i)
	if i != len(data) {
		return messageListing{}
	}
	if deferredContent && (listing.role == "user" || listing.role == "assistant") {
		text, n, ok := scanListingContent(data[contentStart:contentEnd], listing.role)
		if !ok || contentStart+n != contentEnd {
			return messageListing{}
		}
		listing.text = text
	}
	listing.ok = true
	listing.hasContent = sawContent
	listing.hasTimestamp = sawTimestamp
	return listing
}

// skipListingSpace advances past JSON whitespace at i.
func skipListingSpace(data []byte, i int) int {
	for i < len(data) {
		switch data[i] {
		case ' ', '\t', '\r', '\n':
			i++
		default:
			return i
		}
	}
	return i
}

// scanListingInteger consumes a message timestamp. null reads as absent, as
// the typed decoder leaves the zero value; any number it might still accept
// (exponents, fractions, out-of-range values) reports not-ok and falls back.
func scanListingInteger(data []byte) (int64, int, bool) {
	if bytes.HasPrefix(data, []byte("null")) {
		return 0, 4, true
	}
	n, ok := skipJSONValue(data)
	if !ok || n == 0 {
		return 0, 0, false
	}
	number, err := strconv.ParseInt(string(data[:n]), 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return number, n, true
}

// scanListingString consumes one JSON string, unescaping it only when needed.
func scanListingString(data []byte) (string, int, bool) {
	if inner, n, ok := scanPlainStringBytes(data); ok {
		return string(inner), n, true
	}
	n, ok := skipJSONString(data)
	if !ok {
		return "", 0, false
	}
	var text string
	if err := json.Unmarshal(data[:n], &text); err != nil {
		return "", 0, false
	}
	return text, n, true
}

// scanListingContent consumes a user or assistant content value, returning the
// text extractTextContent would join.
func scanListingContent(data []byte, role string) (string, int, bool) {
	i := skipListingSpace(data, 0)
	if i >= len(data) {
		return "", 0, false
	}
	switch data[i] {
	case 'n':
		if bytes.HasPrefix(data[i:], []byte("null")) {
			return "", i + 4, true
		}
		return "", 0, false
	case '"':
		if role != "user" {
			return "", 0, false
		}
		text, n, ok := scanListingString(data[i:])
		if !ok {
			return "", 0, false
		}
		return text, i + n, true
	case '[':
		i++
		var texts []string
		first := true
		for {
			i = skipListingSpace(data, i)
			if i >= len(data) {
				return "", 0, false
			}
			if data[i] == ']' {
				i++
				break
			}
			if !first {
				if data[i] != ',' {
					return "", 0, false
				}
				i++
				i = skipListingSpace(data, i)
				if i < len(data) && data[i] == ']' {
					return "", 0, false
				}
			}
			text, isText, n, ok := scanListingBlock(data[i:])
			if !ok {
				return "", 0, false
			}
			if isText {
				texts = append(texts, text)
			}
			i += n
			first = false
		}
		return strings.Join(texts, " "), i, true
	}
	return "", 0, false
}

// scanListingBlock consumes one content block, returning its text when it is a
// text block. Thinking, image and tool-call blocks are skipped; any other type
// is not understood.
func scanListingBlock(data []byte) (string, bool, int, bool) {
	i := skipListingSpace(data, 0)
	if i >= len(data) || data[i] != '{' {
		return "", false, 0, false
	}
	i++
	var blockType, blockText string
	var sawType, sawText bool
	first := true
	for {
		i = skipListingSpace(data, i)
		if i >= len(data) {
			return "", false, 0, false
		}
		if data[i] == '}' {
			i++
			break
		}
		if !first {
			if data[i] != ',' {
				return "", false, 0, false
			}
			i++
			i = skipListingSpace(data, i)
			if i < len(data) && data[i] == '}' {
				return "", false, 0, false
			}
		}
		key, n, ok := scanPlainStringBytes(data[i:])
		if !ok {
			return "", false, 0, false
		}
		i += n
		i = skipListingSpace(data, i)
		if i >= len(data) || data[i] != ':' {
			return "", false, 0, false
		}
		i++
		i = skipListingSpace(data, i)
		switch string(key) {
		case "type":
			if sawType {
				return "", false, 0, false
			}
			sawType = true
			value, n, ok := scanPlainStringBytes(data[i:])
			if !ok {
				return "", false, 0, false
			}
			blockType = string(value)
			i += n
		case "text":
			if sawText {
				return "", false, 0, false
			}
			sawText = true
			value, n, ok := scanListingString(data[i:])
			if !ok {
				return "", false, 0, false
			}
			blockText = value
			i += n
		default:
			n, ok := skipJSONValue(data[i:])
			if !ok {
				return "", false, 0, false
			}
			i += n
		}
		first = false
	}
	switch blockType {
	case "text":
		return blockText, true, i, true
	case "thinking", "image", "toolCall":
		return "", false, i, true
	}
	return "", false, 0, false
}
