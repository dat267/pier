package durable

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
	"github.com/dat267/pier/chord/services"
)

// Port of session/session.ts: the Session kernel. One mutation line, the
// loaded document tracker cache, and committed publication. Only committed
// state is observable; listeners run after the commit.
//
// The reference serializes asynchronous work through a promise tail and abort
// signals; the port's storage is synchronous, so the line is a mutex and a
// commit callback runs to completion.

// CommitChange is one published change: a table write or a document
// publication (upstream CommitChange).
type CommitChange struct {
	// Write is set for conversation, entry, task and submission writes.
	Write *StorageWrite
	// Document is set for document publications.
	Document *DocumentCommitChange
}

// CommitPublication is what one committed batch publishes (upstream
// CommitPublication).
type CommitPublication struct {
	Seq     Seq
	Changes []CommitChange
}

// CommitListener observes committed publications. It must not call back into
// the session.
type CommitListener func(publication CommitPublication, ctx chord.Context)

// Session is the session kernel over one storage backend.
type Session struct {
	// line serializes commits and read-on-line jobs (the mutation line).
	line sync.Mutex
	// mu guards the documents, listeners, closing flag and poison.
	mu        sync.Mutex
	storage   Storage
	now       func() int64
	documents map[string]*LoadedDocument

	commitListeners map[int]CommitListener
	closeListeners  map[int]func()
	nextListenerID  int

	closing bool
	poison  error

	// ConversationCreatedHook stages the writes every new or forked
	// conversation needs; nil stages nothing.
	ConversationCreatedHook func(tx *Transaction, record ConversationRecord) error
	// BeforeCloseHook runs after close seals admission and before storage
	// closes; it must not fail.
	BeforeCloseHook func() error
}

// NewSession opens a session kernel over one storage backend.
func NewSession(storage Storage) *Session {
	return NewSessionWithClock(storage, nil)
}

// NewSessionWithClock opens a session with an injected wall clock for task
// lifecycle times; nil uses current Unix milliseconds.
func NewSessionWithClock(storage Storage, now func() int64) *Session {
	if now == nil {
		now = wallClockMillis
	}
	return &Session{
		storage:         storage,
		now:             now,
		documents:       map[string]*LoadedDocument{},
		commitListeners: map[int]CommitListener{},
		closeListeners:  map[int]func(){},
	}
}

// Storage exposes the session's storage.
func (s *Session) Storage() Storage { return s.storage }

// Commit runs one commit callback on the mutation line.
func (s *Session) Commit(ctx chord.Context, change func(tx *Transaction) error) error {
	return s.CommitWith(ctx, change, TransactionScope{})
}

// CommitWith runs one commit callback with a transaction scope (default
// task conversation and entry attribution).
func (s *Session) CommitWith(ctx chord.Context, change func(tx *Transaction) error, scope TransactionScope) error {
	return s.runCommit(ctx, change, scope)
}

// ReadOnLine runs a read-only job on the mutation line so multi-read
// derivations observe one committed state.
func (s *Session) ReadOnLine(job func() error) error {
	s.line.Lock()
	defer s.line.Unlock()
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return errors.New("Session is closed")
	}
	if err := s.assertHealthyLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return job()
}

// Snapshot returns the current value of a document, materialized for the
// supplied definition. Args carry the owner id and family key.
func (s *Session) Snapshot(ctx chord.Context, definition DocDefinition, args ...any) (chord.JsonValue, bool, error) {
	s.line.Lock()
	defer s.line.Unlock()
	if err := s.assertUsable(); err != nil {
		return nil, false, err
	}
	resolved, err := ResolveAddress(definition, args...)
	if err != nil {
		return nil, false, err
	}
	loaded, err := s.loadDocumentLocked(definition, resolved.ID, resolved.Address, ctx)
	if err != nil {
		return nil, false, err
	}
	if loaded == nil {
		return nil, false, nil
	}
	if err := CheckRecordScope(definition, SemanticsOfRecord(&loaded.Record)); err != nil {
		return nil, false, err
	}
	if err := CheckRecordVersion(definition, SemanticsOfRecord(&loaded.Record), loaded.StoredVersion); err != nil {
		return nil, false, err
	}
	return loaded.Tracker.Value(), true, nil
}

// ConversationDocumentOnLine returns a conversation document's current
// incarnation and value for a job already on the line.
func (s *Session) ConversationDocumentOnLine(ctx chord.Context, definition DocDefinition, conversationID Id) (*DocumentRecord, int, chord.JsonValue, bool, error) {
	if err := s.assertUsable(); err != nil {
		return nil, 0, nil, false, err
	}
	resolved, err := ResolveAddress(definition, conversationID)
	if err != nil {
		return nil, 0, nil, false, err
	}
	loaded, err := s.loadDocumentLocked(definition, resolved.ID, resolved.Address, ctx)
	if err != nil {
		return nil, 0, nil, false, err
	}
	if loaded == nil {
		return nil, 0, nil, false, nil
	}
	if err := CheckRecordScope(definition, SemanticsOfRecord(&loaded.Record)); err != nil {
		return nil, 0, nil, false, err
	}
	if err := CheckRecordVersion(definition, SemanticsOfRecord(&loaded.Record), loaded.StoredVersion); err != nil {
		return nil, 0, nil, false, err
	}
	record := loaded.Record
	return &record, loaded.ValueVersion, loaded.Tracker.Value(), true, nil
}

// SnapshotAsOf materializes a rewindable conversation document at an entry.
func (s *Session) SnapshotAsOf(ctx chord.Context, definition DocDefinition, conversationID Id, at Id) (chord.JsonValue, bool, error) {
	s.line.Lock()
	defer s.line.Unlock()
	if err := s.assertUsable(); err != nil {
		return nil, false, err
	}
	resolved, err := ResolveAddress(definition, conversationID)
	if err != nil {
		return nil, false, err
	}
	if resolved.Address.Scope.Kind != ScopeConversation {
		return nil, false, errors.New("Session.SnapshotAsOf() requires a conversation document")
	}
	entry, err := visibleEntry(ctx, s.storage, conversationID, at)
	if err != nil {
		return nil, false, err
	}
	if entry == nil {
		return nil, false, fmt.Errorf("Entry %d is not visible from conversation %d", at, conversationID)
	}
	address := resolved.Address
	scope := DocumentScope{Kind: ScopeConversation, ConversationID: &entry.Entry.ConversationID}
	address.Scope = scope
	record, err := s.storage.FindDocument(ctx, address, DocumentPointAt(entry.CommitSeq))
	if err != nil {
		return nil, false, err
	}
	if record == nil {
		return nil, false, nil
	}
	stored, err := s.storage.Document(ctx, record.ID, DocumentPointAt(entry.CommitSeq))
	if err != nil {
		return nil, false, err
	}
	if stored == nil {
		return nil, false, fmt.Errorf("Historical document %d (%s) cannot be read", record.ID, record.Kind)
	}
	value, err := MaterializeDocument(definition, stored)
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

// DocumentState attaches a replicated state to one committed incarnation.
func (s *Session) DocumentState(ctx chord.Context, definition DocDefinition, args ...any) (*services.AttachedState[chord.JsonValue], bool, error) {
	s.line.Lock()
	defer s.line.Unlock()
	if err := s.assertUsable(); err != nil {
		return nil, false, err
	}
	resolved, err := ResolveAddress(definition, args...)
	if err != nil {
		return nil, false, err
	}
	loaded, err := s.loadDocumentLocked(definition, resolved.ID, resolved.Address, ctx)
	if err != nil {
		return nil, false, err
	}
	if loaded == nil {
		return nil, false, nil
	}
	observer, _, err := s.attachDocumentLocked(definition, loaded, committedObserverState)
	if err != nil {
		return nil, false, err
	}
	source, ok := observer.(*CommittedStateSource[chord.JsonValue])
	if !ok {
		return nil, false, errors.New("document state observer mismatch")
	}
	state := services.NewAttachedState[chord.JsonValue](source, nil)
	return state, true, nil
}

// WatchDoc attaches an exact-frame watch to one committed incarnation.
func (s *Session) WatchDoc(ctx chord.Context, definition DocDefinition, args ...any) (DocumentWatch[chord.JsonValue], bool, error) {
	s.line.Lock()
	defer s.line.Unlock()
	if err := s.assertUsable(); err != nil {
		return nil, false, err
	}
	resolved, err := ResolveAddress(definition, args...)
	if err != nil {
		return nil, false, err
	}
	loaded, err := s.loadDocumentLocked(definition, resolved.ID, resolved.Address, ctx)
	if err != nil {
		return nil, false, err
	}
	if loaded == nil {
		return nil, false, nil
	}
	observer, _, err := s.attachDocumentLocked(definition, loaded, committedObserverWatch)
	if err != nil {
		return nil, false, err
	}
	watch, _ := observer.(DocumentWatch[chord.JsonValue])
	if ctx != nil && ctx.Done() != nil {
		if watchTyped, ok := watch.(*CommittedWatch[chord.JsonValue]); ok {
			watchTyped.ObserveCancellation(ctx)
		}
	}
	return watch, true, nil
}

// Close seals admission, drops observers, and closes storage. The before-close
// hook runs after admission is sealed and before storage closes.
func (s *Session) Close(ctx chord.Context) error {
	s.line.Lock()
	defer s.line.Unlock()
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return nil
	}
	s.closing = true
	beforeClose := s.BeforeCloseHook
	listeners := make([]func(), 0, len(s.closeListeners))
	for _, listener := range s.closeListeners {
		listeners = append(listeners, listener)
	}
	s.closeListeners = map[int]func(){}
	s.mu.Unlock()
	for _, listener := range listeners {
		listener()
	}
	if beforeClose != nil {
		_ = beforeClose()
	}
	s.mu.Lock()
	s.commitListeners = map[int]CommitListener{}
	s.documents = map[string]*LoadedDocument{}
	s.mu.Unlock()
	return s.storage.Close(ctx)
}

// SubscribeCommits registers a post-adoption listener.
func (s *Session) SubscribeCommits(listener CommitListener) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		panic("Session is closed")
	}
	id := s.nextListenerID
	s.nextListenerID++
	s.commitListeners[id] = listener
	return func() {
		s.mu.Lock()
		delete(s.commitListeners, id)
		s.mu.Unlock()
	}
}

// SubscribeClose registers a listener called when close begins.
func (s *Session) SubscribeClose(listener func()) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		panic("Session is closed")
	}
	id := s.nextListenerID
	s.nextListenerID++
	s.closeListeners[id] = listener
	return func() {
		s.mu.Lock()
		delete(s.closeListeners, id)
		s.mu.Unlock()
	}
}

// UnloadDocuments drops every loaded tracker; later access cold-loads.
func (s *Session) UnloadDocuments() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.documents = map[string]*LoadedDocument{}
}

// ─── Internals ──────────────────────────────────────────────────────────────

// sessionHost implements TransactionHost over the session.
type sessionHost struct{ session *Session }

func wallClockMillis() int64 { return time.Now().UnixMilli() }

func (h sessionHost) Storage() Storage { return h.session.storage }

func (h sessionHost) Now() int64 { return h.session.now() }

func (h sessionHost) Cached(addressID string) *LoadedDocument {
	h.session.mu.Lock()
	defer h.session.mu.Unlock()
	return h.session.documents[addressID]
}

func (h sessionHost) Load(ctx chord.Context, definition DocDefinition, addressID string, address DocumentAddress) (*LoadedDocument, error) {
	// loadDocumentLocked takes the state lock itself.
	return h.session.loadDocumentLocked(definition, addressID, address, ctx)
}

func (h sessionHost) Install(document LoadedDocument) {
	h.session.mu.Lock()
	defer h.session.mu.Unlock()
	copied := document
	h.session.documents[document.AddressID] = &copied
}

func (h sessionHost) Evict(addressID string, recordID Id) {
	h.session.mu.Lock()
	defer h.session.mu.Unlock()
	if document := h.session.documents[addressID]; document != nil && document.Record.ID == recordID {
		delete(h.session.documents, addressID)
	}
}

func (h sessionHost) ConversationCreated(tx *Transaction, record ConversationRecord) error {
	if h.session.ConversationCreatedHook != nil {
		return h.session.ConversationCreatedHook(tx, record)
	}
	return nil
}

func (s *Session) runCommit(ctx chord.Context, change func(tx *Transaction) error, scope TransactionScope) error {
	// The mutation line: the transaction, its admission and its publication run
	// to completion before another commit starts (the port's synchronous
	// equivalent of upstream's promise tail). It is a separate mutex from the
	// state lock, which the transaction host re-enters.
	s.line.Lock()
	defer s.line.Unlock()
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return errors.New("Session is closed")
	}
	if err := s.assertHealthyLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	tx := NewTransaction(sessionHost{s}, ctx, scope)
	if err := change(tx); err != nil {
		tx.SettleFailure()
		return err
	}
	writes, err := tx.SettleSuccess()
	if err != nil {
		return err
	}
	if len(writes) == 0 {
		tx.Discard()
		return nil
	}
	seq, err := s.storage.Commit(ctx, writes)
	if err != nil {
		tx.Discard()
		if !isStorageRejected(err) {
			s.poison = err
		}
		return err
	}
	documents := tx.Adopt(seq)
	s.publish(seq, writes, documents, ctx)
	return nil
}

func (s *Session) publish(seq Seq, writes []StorageWrite, documents []DocumentCommitChange, ctx chord.Context) {
	s.mu.Lock()
	if len(s.commitListeners) == 0 {
		s.mu.Unlock()
		return
	}
	changes := make([]CommitChange, 0, len(writes)+len(documents))
	for index := range writes {
		switch writes[index].Type {
		case "conversation", "entry", "task", "submission":
			write := writes[index]
			changes = append(changes, CommitChange{Write: &write})
		}
	}
	for index := range documents {
		document := documents[index]
		changes = append(changes, CommitChange{Document: &document})
	}
	publication := CommitPublication{Seq: seq, Changes: changes}
	listeners := make([]CommitListener, 0, len(s.commitListeners))
	for _, listener := range s.commitListeners {
		listeners = append(listeners, listener)
	}
	s.mu.Unlock()
	// Deliver outside the state lock; listeners must not re-enter the session.
	for _, listener := range listeners {
		listener(publication, ctx)
	}
}

type committedObserverKind int

const (
	committedObserverState committedObserverKind = iota
	committedObserverWatch
)

// attachDocumentLocked attaches an observer to one committed incarnation.
func (s *Session) attachDocumentLocked(
	definition DocDefinition,
	loaded *LoadedDocument,
	kind committedObserverKind,
) (any, func(), error) {
	if err := CheckRecordScope(definition, SemanticsOfRecord(&loaded.Record)); err != nil {
		return nil, nil, err
	}
	if err := CheckRecordVersion(definition, SemanticsOfRecord(&loaded.Record), loaded.StoredVersion); err != nil {
		return nil, nil, err
	}
	observed := &observedVersion{version: loaded.ValueVersion}
	recordID := loaded.Record.ID

	var commitID, closeID int
	detach := func() {
		s.mu.Lock()
		delete(s.commitListeners, commitID)
		delete(s.closeListeners, closeID)
		s.mu.Unlock()
	}

	var observer any
	if kind == committedObserverState {
		source := NewCommittedStateSource[chord.JsonValue](loaded.Tracker.Value(), detach)
		observer = source
	} else {
		watch := NewCommittedWatch[chord.JsonValue](loaded.Tracker.Value(), detach, nil)
		observer = watch
	}

	s.mu.Lock()
	commitID = s.nextListenerID
	s.nextListenerID++
	s.commitListeners[commitID] = func(publication CommitPublication, ctx chord.Context) {
		for _, change := range publication.Changes {
			if change.Document == nil || change.Document.Record.ID != recordID {
				continue
			}
			ops := observedOperations(observed, *change.Document)
			if len(ops) == 0 {
				continue
			}
			switch typed := observer.(type) {
			case *CommittedStateSource[chord.JsonValue]:
				typed.Advance(change.Document.Value, ops, ctx)
			case *CommittedWatch[chord.JsonValue]:
				typed.Advance(change.Document.Value, ops, ctx)
			}
		}
	}
	closeID = s.nextListenerID
	s.nextListenerID++
	s.closeListeners[closeID] = func() {
		switch typed := observer.(type) {
		case *CommittedStateSource[chord.JsonValue]:
			typed.CloseSession()
		case *CommittedWatch[chord.JsonValue]:
			typed.CloseSession()
		}
	}
	s.mu.Unlock()
	if kind == committedObserverState {
		state := observer.(*CommittedStateSource[chord.JsonValue])
		return state, detach, nil
	}
	return observer, detach, nil
}

// observedVersion tracks the definition version an observer hydrated under.
type observedVersion struct{ version int }

// observedOperations computes the ops one committed change delivers to an
// observer (upstream observedOperations).
func observedOperations(observed *observedVersion, change DocumentCommitChange) []delta.Op {
	if change.Value == nil {
		return RetirementOperations
	}
	if change.Version != nil && *change.Version == observed.version {
		return change.Ops
	}
	if change.Version != nil {
		observed.version = *change.Version
	}
	return []delta.Op{{Verb: delta.VerbReplace, Value: change.Value}}
}

// loadDocumentLocked returns the cached incarnation or cold-loads and migrates
// it for the definition's version.
func (s *Session) loadDocumentLocked(definition DocDefinition, addressID string, address DocumentAddress, ctx chord.Context) (*LoadedDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached := s.documents[addressID]; cached != nil {
		if cached.ValueVersion == definition.Version {
			return cached, nil
		}
		delete(s.documents, addressID)
	}
	record, err := s.storage.FindDocument(ctx, address, CurrentDocumentPoint())
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, nil
	}
	stored, err := s.storage.Document(ctx, record.ID, CurrentDocumentPoint())
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, fmt.Errorf("Current document %d (%s) cannot be read", record.ID, record.Kind)
	}
	value, err := MaterializeDocument(definition, stored)
	if err != nil {
		return nil, err
	}
	loaded := &LoadedDocument{
		AddressID:       addressID,
		Record:          stored.Record,
		StoredVersion:   stored.Version,
		ValueVersion:    definition.Version,
		DeltasSinceBase: stored.DeltasSinceBase,
		Tracker:         delta.Track(value),
	}
	s.documents[addressID] = loaded
	return loaded, nil
}

// assertUsable rejects use after close or after a poisoned commit.
func (s *Session) assertUsable() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return errors.New("Session is closed")
	}
	return s.assertHealthyLocked()
}

func (s *Session) assertHealthyLocked() error {
	if s.poison != nil {
		return fmt.Errorf("Session is poisoned by a failed commit after storage admission; reopen it (cause: %v)", s.poison)
	}
	return nil
}

func isStorageRejected(err error) bool {
	var rejected *StorageRejectedError
	return errors.As(err, &rejected)
}
