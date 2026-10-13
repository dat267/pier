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

// SessionEnd reasons (upstream SessionEnd).
const (
	SessionReasonClosed = "closed"
	SessionReasonFailed = "failed"
)

// SessionEnd is how a Session settled: closed normally, or failed with the
// storage error that failed it.
type SessionEnd struct {
	Reason string
	// Err is the cause for SessionReasonFailed.
	Err error
}

// Session is the session kernel over one storage backend.
type Session struct {
	// line serializes commits and read-on-line jobs (the mutation line).
	line sync.Mutex
	// mu guards the documents, listeners, closing flag and poison.
	mu        sync.Mutex
	storage   Storage
	now       func() int64
	documents map[string]*LoadedDocument
	// documentRetention returns how long an unobserved loaded document stays in
	// memory after its last use; nil keeps the default.
	documentRetention func() int
	// attached counts observers per document incarnation (record ID).
	attached map[Id]int
	// idle maps an unobserved cached document's address to its last-use time.
	idle map[string]int64

	commitListeners map[int]CommitListener
	// internalListeners are the Session's own observers (scheduler, views,
	// task graph, submissions); a throw in one fails the Session, unlike a host
	// listener's, which is reported.
	internalListeners map[int]CommitListener
	closeListeners    map[int]func()
	nextListenerID    int

	// report receives failures that do not fail the caller, such as a throwing
	// host listener. A plain Session drops them; the Harness overrides it.
	report func(error)

	closing bool
	poison  error
	// closed settles once the Session has closed, Storage included; end holds
	// its result once closed is closed.
	closed chan struct{}
	end    *SessionEnd

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
		storage:           storage,
		now:               now,
		documents:         map[string]*LoadedDocument{},
		attached:          map[Id]int{},
		idle:              map[string]int64{},
		commitListeners:   map[int]CommitListener{},
		internalListeners: map[int]CommitListener{},
		closeListeners:    map[int]func(){},
		report:            func(error) {},
		closed:            make(chan struct{}),
	}
}

// Storage exposes the session's storage.
func (s *Session) Storage() Storage {
	return sessionStorageGuard{Storage: s.storage, check: s.assertUsable, fail: s.storageFailed, failCommit: s.storageCommitFailed}
}

// Closed settles once the Session has closed, Storage included.
func (s *Session) Closed() <-chan struct{} { return s.closed }

// End is the terminal result, available once Closed has settled.
func (s *Session) End() (SessionEnd, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.end == nil {
		return SessionEnd{}, false
	}
	return *s.end, true
}

// Failure reports the cause that failed the Session, if any.
func (s *Session) Failure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poison == nil {
		return nil
	}
	return &SessionFailedError{Cause: s.poison}
}

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
	if err := s.assertHealthyLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	if s.closing {
		s.mu.Unlock()
		return errors.New("Session is closed")
	}
	s.mu.Unlock()
	if err := job(); err != nil {
		return err
	}
	s.evictIdleDocuments()
	s.mu.Lock()
	err := s.assertHealthyLocked()
	s.mu.Unlock()
	return err
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
	// One failing listener is reported and the rest still run.
	for _, listener := range listeners {
		listener := listener
		contained(listener, s.report)
	}
	if beforeClose != nil {
		_ = beforeClose()
	}
	s.mu.Lock()
	s.commitListeners = map[int]CommitListener{}
	s.internalListeners = map[int]CommitListener{}
	s.documents = map[string]*LoadedDocument{}
	s.attached = map[Id]int{}
	s.idle = map[string]int64{}
	s.mu.Unlock()
	err := s.storage.Close(ctx)
	// A Storage that cannot close is a failed one; an earlier failure stays the
	// cause. `closed` settles either way.
	if err != nil {
		s.mu.Lock()
		if s.poison == nil {
			s.poison = err
			s.mu.Unlock()
			s.report(err)
			s.mu.Lock()
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	if s.end == nil {
		end := SessionEnd{Reason: SessionReasonClosed}
		if s.poison != nil {
			end = SessionEnd{Reason: SessionReasonFailed, Err: s.poison}
		}
		s.end = &end
		close(s.closed)
	}
	s.mu.Unlock()
	return err
}

// SubscribeCommits registers a post-adoption host listener. It must not call
// back into the Session; a panic is reported instead of failing the commit.
func (s *Session) SubscribeCommits(listener CommitListener) func() {
	return s.addCommitListener(&s.commitListeners, listener)
}

// ObserveCommits registers one of the Session's own post-adoption listeners.
// It must not call back into the Session; a panic fails the Session, because
// such a listener keeps in-memory state in step with storage.
func (s *Session) ObserveCommits(listener CommitListener) func() {
	return s.addCommitListener(&s.internalListeners, listener)
}

func (s *Session) addCommitListener(set *map[int]CommitListener, listener CommitListener) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		panic("Session is closed")
	}
	if *set == nil {
		*set = map[int]CommitListener{}
	}
	id := s.nextListenerID
	s.nextListenerID++
	(*set)[id] = listener
	return func() {
		s.mu.Lock()
		delete(*set, id)
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

func (h sessionHost) Storage() Storage { return h.session.Storage() }

func (h sessionHost) Now() int64 { return h.session.now() }

func (h sessionHost) Cached(addressID string) *LoadedDocument {
	h.session.mu.Lock()
	defer h.session.mu.Unlock()
	document := h.session.documents[addressID]
	if document != nil {
		h.session.touchDocumentLocked(addressID, document.Record.ID)
	}
	return document
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
	h.session.touchDocumentLocked(document.AddressID, document.Record.ID)
}

func (h sessionHost) Evict(addressID string, recordID Id) {
	h.session.mu.Lock()
	defer h.session.mu.Unlock()
	if document := h.session.documents[addressID]; document != nil && document.Record.ID == recordID {
		delete(h.session.documents, addressID)
		delete(h.session.idle, addressID)
		delete(h.session.attached, recordID)
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
	if err := s.assertHealthyLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	if s.closing {
		s.mu.Unlock()
		return errors.New("Session is closed")
	}
	s.mu.Unlock()
	tx := NewTransaction(sessionHost{s}, ctx, scope)
	if err := change(tx); err != nil {
		tx.SettleFailure()
		return err
	}
	s.mu.Lock()
	failure := s.assertHealthyLocked()
	s.mu.Unlock()
	if failure != nil {
		tx.SettleFailure()
		return failure
	}
	writes, err := tx.SettleSuccess()
	if err != nil {
		return err
	}
	if len(writes) == 0 {
		tx.Discard()
		s.evictIdleDocuments()
		return nil
	}
	seq, err := s.storage.Commit(ctx, writes)
	if err != nil {
		tx.Discard()
		s.storageCommitFailed(err)
		return err
	}
	documents := tx.Adopt(seq)
	s.publish(seq, writes, documents, ctx)
	s.evictIdleDocuments()
	return nil
}

func (s *Session) publish(seq Seq, writes []StorageWrite, documents []DocumentCommitChange, ctx chord.Context) {
	s.mu.Lock()
	if len(s.commitListeners) == 0 && len(s.internalListeners) == 0 {
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
	internal := make([]CommitListener, 0, len(s.internalListeners))
	for _, listener := range s.internalListeners {
		internal = append(internal, listener)
	}
	listeners := make([]CommitListener, 0, len(s.commitListeners))
	for _, listener := range s.commitListeners {
		listeners = append(listeners, listener)
	}
	s.mu.Unlock()
	// Deliver outside the state lock; listeners must not re-enter the session.
	// The commit is durable, so a failing listener neither fails the commit nor
	// skips the others; an internal listener's throw fails the Session instead.
	for _, listener := range internal {
		listener := listener
		contained(func() { listener(publication, ctx) }, s.fail)
	}
	for _, listener := range listeners {
		listener := listener
		contained(func() { listener(publication, ctx) }, s.report)
	}
}

// fail fails the Session with the first error and reports it once; admission
// ends and close listeners run. Storage guard failures and a throw in one of
// the Session's own listeners call it.
func (s *Session) fail(err error) {
	s.mu.Lock()
	if s.poison != nil {
		s.mu.Unlock()
		return
	}
	s.poison = err
	s.mu.Unlock()
	go func() { _ = s.Close(chord.BackgroundContext) }()
	s.report(err)
}

// contained runs a host callback that must not fail its caller: a panic goes
// to onError, which itself must not panic (safeReport wraps onReport).
func contained(callback func(), onError func(error)) {
	defer func() {
		if recovered := recover(); recovered != nil {
			onError(panicError(recovered))
		}
	}()
	callback()
}

// panicError converts a recovered value into an error.
func panicError(recovered any) error {
	if err, ok := recovered.(error); ok {
		return err
	}
	return fmt.Errorf("%v", recovered)
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
		if s.attached[recordID] > 0 {
			s.attached[recordID]--
		}
		if s.attached[recordID] == 0 {
			s.idle[loaded.AddressID] = s.now()
		}
		s.mu.Unlock()
	}

	var observer any
	if kind == committedObserverState {
		source := NewCommittedStateSource[chord.JsonValue](loaded.Tracker.Value(), detach)
		observer = source
	} else {
		watch := NewCommittedWatch[chord.JsonValue](loaded.Tracker.Value(), detach, nil)
		watch.SetReport(s.report)
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
		failure := s.Failure()
		switch typed := observer.(type) {
		case *CommittedStateSource[chord.JsonValue]:
			typed.CloseSession()
		case *CommittedWatch[chord.JsonValue]:
			if failure != nil {
				typed.FailSession(failure)
			} else {
				typed.CloseSession()
			}
		}
	}
	s.attached[recordID]++
	delete(s.idle, loaded.AddressID)
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

// evictIdleDocuments drops expired unobserved cached documents, taking the
// state lock itself.
func (s *Session) evictIdleDocuments() {
	s.mu.Lock()
	s.evictIdleDocumentsLocked()
	s.mu.Unlock()
}

// retentionMs resolves the configured document retention.
func (s *Session) retentionMs() int {
	if s.documentRetention == nil {
		return DefaultDocumentRetentionMs
	}
	return s.documentRetention()
}

// touchDocumentLocked notes a use of a cached document: an unobserved one
// becomes idle from now, an observed one is never idle. The caller holds s.mu.
func (s *Session) touchDocumentLocked(addressID string, recordID Id) {
	if s.attached[recordID] > 0 {
		delete(s.idle, addressID)
		return
	}
	s.idle[addressID] = s.now()
}

// evictIdleDocumentsLocked drops the unobserved cached documents whose
// retention passed. The caller holds s.mu.
func (s *Session) evictIdleDocumentsLocked() {
	retention := int64(s.retentionMs())
	now := s.now()
	for addressID, since := range s.idle {
		document := s.documents[addressID]
		if document == nil {
			delete(s.idle, addressID)
			continue
		}
		if s.attached[document.Record.ID] > 0 {
			delete(s.idle, addressID)
			continue
		}
		if retention > 0 && now-since < retention {
			continue
		}
		delete(s.documents, addressID)
		delete(s.idle, addressID)
	}
}

// loadDocumentLocked returns the cached incarnation or cold-loads and migrates
// it for the definition's version.
func (s *Session) loadDocumentLocked(definition DocDefinition, addressID string, address DocumentAddress, ctx chord.Context) (*LoadedDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached := s.documents[addressID]; cached != nil {
		if cached.ValueVersion == definition.Version {
			s.touchDocumentLocked(addressID, cached.Record.ID)
			return cached, nil
		}
		delete(s.documents, addressID)
		delete(s.idle, addressID)
		delete(s.attached, cached.Record.ID)
	}
	record, err := s.storage.FindDocument(ctx, address, CurrentDocumentPoint())
	if err != nil {
		s.markStorageFailureLocked(err)
		return nil, err
	}
	if record == nil {
		return nil, nil
	}
	stored, err := s.storage.Document(ctx, record.ID, CurrentDocumentPoint())
	if err != nil {
		s.markStorageFailureLocked(err)
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
	s.touchDocumentLocked(addressID, loaded.Record.ID)
	return loaded, nil
}

// assertUsable rejects use after close or after a poisoned commit.
func (s *Session) assertUsable() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.assertHealthyLocked(); err != nil {
		return err
	}
	if s.closing {
		return errors.New("Session is closed")
	}
	return nil
}

func (s *Session) assertHealthyLocked() error {
	if s.poison != nil {
		return &SessionFailedError{Cause: s.poison}
	}
	return nil
}

func (s *Session) storageFailed(err error) {
	s.mu.Lock()
	s.markStorageFailureLocked(err)
	s.mu.Unlock()
}

func (s *Session) storageCommitFailed(err error) {
	s.mu.Lock()
	s.markStorageCommitFailureLocked(err)
	s.mu.Unlock()
}

// markStorageFailureLocked fails the Session on its first non-rejection Storage
// error. The caller holds s.mu. Close waits for the current mutation line to
// finish, then releases the backend and rejects close subscribers.
func (s *Session) markStorageFailureLocked(err error) {
	if err == nil || isStorageRejected(err) || s.poison != nil {
		return
	}
	s.poison = err
	go func() { _ = s.Close(chord.BackgroundContext) }()
}

func (s *Session) markStorageCommitFailureLocked(err error) {
	if err == nil || s.poison != nil {
		return
	}
	s.poison = err
	go func() { _ = s.Close(chord.BackgroundContext) }()
}

func isStorageRejected(err error) bool {
	var rejected *StorageRejectedError
	return errors.As(err, &rejected)
}
