package durable

import (
	"fmt"
	"sync"

	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
	"github.com/dat267/pier/chord/services"
)

// Port of harness/view.ts: the structural mount of one conversation's active
// transcript and built-in documents.

// JsonObject is a JSON object value (upstream JsonObject).
type JsonObject = map[string]any

// ConversationView is the structural mount of one conversation (spec §9.3).
type ConversationView struct {
	Conversation ConversationRecord
	// Entries are the raw active entries, as ContextView.Entries.
	Entries []EntryRecord
	// Docs are pi.agent, pi.live, pi.inbox and pi.usage keyed by kind; absent
	// documents are absent.
	Docs map[string]chord.JsonValue
}

// ViewObserver receives each next revision of a mount and the Session's close.
// CloseSession is required; Advance and Publication are optional.
type ViewObserver interface {
	CloseSession()
}

// ViewAdvancer receives each next revision of a mount.
type ViewAdvancer interface {
	Advance(value ConversationView, ops []delta.Op, ctx chord.Context)
}

// ViewPublisher receives every publication, after the mount took it.
type ViewPublisher interface {
	Publication(before, after ConversationView, ops []delta.Op, publication CommitPublication, ctx chord.Context)
}

// mountedDocs are the built-in documents a mount shows.
var mountedDocs = []DocToken{AgentDoc, LiveDoc, InboxDoc, UsageDoc}

func mountedDocKinds() map[string]bool {
	kinds := map[string]bool{}
	for _, token := range mountedDocs {
		kinds[token.Definition.Kind] = true
	}
	return kinds
}

var mountedDocKindSet = mountedDocKinds()

// viewIncarnation is a mounted incarnation and definition version per kind.
type viewIncarnation struct {
	ID      Id
	Version int
}

// viewMount is one conversation's mount.
type viewMount struct {
	value     ConversationView
	docs      map[string]viewIncarnation
	observers map[int]ViewObserver
	nextID    int
}

// ConversationViews holds the harness's conversation view mounts: at most one
// per conversation, built on the Session line by its first observer and
// dropped with its last.
type ConversationViews struct {
	session *Session
	storage Storage

	mu     sync.Mutex
	mounts map[Id]*viewMount
	closed bool
}

// conversationViewsRegistry maps a session to its mounts (upstream the
// module-private WeakMap).
var conversationViewsRegistry sync.Map

// NewConversationViews builds the mounts of one session and subscribes it to
// the session's publications.
func NewConversationViews(session *Session, storage Storage) *ConversationViews {
	views := &ConversationViews{session: session, storage: storage, mounts: map[Id]*viewMount{}}
	conversationViewsRegistry.Store(session, views)
	session.SubscribeCommits(func(publication CommitPublication, ctx chord.Context) {
		views.advanceAll(publication, ctx)
	})
	session.SubscribeClose(func() {
		views.mu.Lock()
		views.closed = true
		mounts := make([]*viewMount, 0, len(views.mounts))
		for _, mount := range views.mounts {
			mounts = append(mounts, mount)
		}
		views.mounts = map[Id]*viewMount{}
		views.mu.Unlock()
		for _, mount := range mounts {
			observers := snapshotObservers(mount)
			for _, observer := range observers {
				observer.CloseSession()
			}
		}
	})
	return views
}

// ConversationViewsFor returns the mounts registered for a harness
// (upstream conversationViews).
func ConversationViewsFor(harness *Session) (*ConversationViews, error) {
	value, ok := conversationViewsRegistry.Load(harness)
	if !ok {
		return nil, fmt.Errorf("Not a Harness")
	}
	return value.(*ConversationViews), nil
}

func (v *ConversationViews) isClosed() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.closed
}

// State is a disposable read-only Chord state of the view.
func (v *ConversationViews) State(id Id, ctx chord.Context) (*services.AttachedState[ConversationView], error) {
	var source *CommittedStateSource[ConversationView]
	_, _, err := v.Attach(id, func(value ConversationView, release func(), _ Storage) (ViewObserver, error) {
		source = NewCommittedStateSource(value, release)
		return source, nil
	}, ctx)
	if err != nil {
		return nil, err
	}
	return services.NewAttachedState[ConversationView](source, nil), nil
}

// Watch is a serialized exact-frame watch of the view; cancelling ctx stops it.
func (v *ConversationViews) Watch(id Id, ctx chord.Context) (WatchHandle[ConversationView], error) {
	var watch *CommittedWatch[ConversationView]
	_, _, err := v.Attach(id, func(value ConversationView, release func(), _ Storage) (ViewObserver, error) {
		watch = NewCommittedWatch(value, release, nil)
		return watch, nil
	}, ctx)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		watch.Cancel()
		return nil, ctx.Err()
	}
	watch.ObserveCancellation(ctx)
	return watch, nil
}

// Attach registers an observer created from the current revision, atomically
// on the Session line. release drops it and the mount with its last observer.
func (v *ConversationViews) Attach(
	id Id,
	create func(value ConversationView, release func(), storage Storage) (ViewObserver, error),
	ctx chord.Context,
) (ViewObserver, func(), error) {
	var observer ViewObserver
	var detach func()
	err := v.session.ReadOnLine(func() error {
		v.mu.Lock()
		mount := v.mounts[id]
		v.mu.Unlock()
		if mount == nil {
			built, err := v.build(id, ctx)
			if err != nil {
				return err
			}
			mount = built
		}
		v.mu.Lock()
		observerID := mount.nextID
		mount.nextID++
		v.mu.Unlock()
		release := func() {
			v.mu.Lock()
			defer v.mu.Unlock()
			delete(mount.observers, observerID)
			if len(mount.observers) == 0 && v.mounts[id] == mount {
				delete(v.mounts, id)
			}
		}
		created, err := create(mount.value, release, v.storage)
		if err != nil {
			return err
		}
		observer = created
		v.mu.Lock()
		closed := v.closed
		v.mu.Unlock()
		if closed {
			return ClosedError()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		v.mu.Lock()
		v.mounts[id] = mount
		mount.observers[observerID] = observer
		v.mu.Unlock()
		detach = release
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return observer, detach, nil
}

func (v *ConversationViews) build(id Id, ctx chord.Context) (*viewMount, error) {
	conversation, err := v.storage.Conversation(ctx, id)
	if err != nil {
		return nil, err
	}
	if conversation == nil {
		return nil, fmt.Errorf("Conversation %s does not exist", itoaID(id))
	}
	bounds, err := CaptureContextBounds(ctx, v.storage, id, nil)
	if err != nil {
		return nil, err
	}
	entries, err := ActiveEntries(ctx, v.storage, id, bounds)
	if err != nil {
		return nil, err
	}
	docs := map[string]chord.JsonValue{}
	incarnations := map[string]viewIncarnation{}
	for _, token := range mountedDocs {
		record, version, value, ok, err := v.session.ConversationDocumentOnLine(ctx, token.Definition, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		docs[token.Definition.Kind] = value
		incarnations[token.Definition.Kind] = viewIncarnation{ID: record.ID, Version: version}
	}
	return &viewMount{
		value: ConversationView{Conversation: *conversation, Entries: entries, Docs: docs},
		docs:  incarnations, observers: map[int]ViewObserver{},
	}, nil
}

func (v *ConversationViews) advanceAll(publication CommitPublication, ctx chord.Context) {
	v.mu.Lock()
	mounts := make(map[Id]*viewMount, len(v.mounts))
	for id, mount := range v.mounts {
		mounts[id] = mount
	}
	v.mu.Unlock()
	for id, mount := range mounts {
		v.advance(id, mount, publication, ctx)
	}
}

// advance derives the mount's operations from one publication, applies them
// and hands the revision to every observer.
func (v *ConversationViews) advance(id Id, mount *viewMount, publication CommitPublication, ctx chord.Context) {
	v.mu.Lock()
	var docOps []delta.Op
	var entryOps []delta.Op
	entries := mount.value.Entries
	for _, change := range publication.Changes {
		if change.Write != nil && change.Write.Type == "entry" && change.Write.Entry != nil &&
			change.Write.Entry.ConversationID == id {
			entry := change.Write.Entry
			value, err := jsonValueOf(*entry)
			if err != nil {
				continue
			}
			if entry.Head == nil {
				entryOps = append(entryOps, delta.Op{
					Verb: delta.VerbSplice, Path: delta.Path{"entries"},
					Index: len(entries), Remove: 0, Items: []chord.JsonValue{value},
				})
				entries = append(entries, *entry)
				continue
			}
			target := *entry.Head
			kept := len(entries)
			for index, candidate := range entries {
				if candidate.Head == nil && candidate.ID >= target {
					kept = index
					break
				}
			}
			entryOps = append(entryOps, delta.Op{
				Verb: delta.VerbSplice, Path: delta.Path{"entries"},
				Index: 0, Remove: kept, Items: []chord.JsonValue{value},
			})
			head := make([]EntryRecord, 0, len(entries)-kept+1)
			head = append(head, *entry)
			head = append(head, entries[kept:]...)
			entries = head
			continue
		}
		document := change.Document
		if document == nil || document.Type != "document" ||
			document.ConversationID == nil || *document.ConversationID != id {
			continue
		}
		kind := document.Record.Kind
		if !mountedDocKindSet[kind] || document.Record.Key != nil {
			continue
		}
		path := delta.Path{"docs", kind}
		mounted, isMounted := mount.docs[kind]
		if document.Value == nil {
			if !isMounted || mounted.ID != document.Record.ID {
				continue
			}
			delete(mount.docs, kind)
			docOps = append(docOps, delta.Op{Verb: delta.VerbDelete, Path: path})
			continue
		}
		if isMounted && mounted.ID == document.Record.ID && document.Version != nil && mounted.Version == *document.Version {
			for _, op := range document.Ops {
				docOps = append(docOps, prefixedOp(op, path))
			}
			continue
		}
		if document.Version == nil {
			continue
		}
		mount.docs[kind] = viewIncarnation{ID: document.Record.ID, Version: *document.Version}
		docOps = append(docOps, delta.Op{Verb: delta.VerbSet, Path: path, Value: document.Value})
	}
	before := mount.value
	ops := make([]delta.Op, 0, len(docOps)+len(entryOps))
	ops = append(ops, docOps...)
	ops = append(ops, entryOps...)
	after := mount.value
	if len(ops) > 0 {
		if len(docOps) > 0 {
			docs := make(map[string]chord.JsonValue, len(mount.value.Docs))
			for key, value := range mount.value.Docs {
				docs[key] = value
			}
			applyDocsOps(docs, docOps)
			after.Docs = docs
		}
		after.Entries = entries
		mount.value = after
	}
	observers := snapshotObservers(mount)
	v.mu.Unlock()

	if len(ops) == 0 {
		return
	}
	frameContext := chord.WithoutAbortSignal(ctx)
	for _, observer := range observers {
		if advancer, ok := observer.(ViewAdvancer); ok {
			advancer.Advance(mount.value, ops, frameContext)
		}
	}
	for _, observer := range observers {
		if publisher, ok := observer.(ViewPublisher); ok {
			publisher.Publication(before, mount.value, ops, publication, frameContext)
		}
	}
}

// applyDocsOps applies the prefixed document operations to the docs map.
func applyDocsOps(docs map[string]chord.JsonValue, ops []delta.Op) {
	for _, op := range ops {
		if len(op.Path) < 2 {
			continue
		}
		kind, ok := op.Path[1].(string)
		if !ok {
			continue
		}
		if len(op.Path) == 2 {
			switch op.Verb {
			case delta.VerbSet:
				docs[kind] = op.Value
				continue
			case delta.VerbDelete:
				delete(docs, kind)
				continue
			}
		}
		sub := op
		sub.Path = op.Path[2:]
		value, err := delta.ApplyImmutable(docs[kind], []delta.Op{sub})
		if err != nil {
			continue
		}
		docs[kind] = value
	}
}

// prefixedOp moves an op under prefix; a root replacement becomes a set of the
// prefix.
func prefixedOp(op delta.Op, prefix delta.Path) delta.Op {
	at := func(path delta.Path) delta.Path {
		joined := make(delta.Path, 0, len(prefix)+len(path))
		joined = append(joined, prefix...)
		joined = append(joined, path...)
		return joined
	}
	switch op.Verb {
	case delta.VerbReplace:
		return delta.Op{Verb: delta.VerbSet, Path: prefix, Value: op.Value}
	case delta.VerbSplice:
		return delta.Op{Verb: op.Verb, Path: at(op.Path), Index: op.Index, Remove: op.Remove, Items: op.Items}
	case delta.VerbSet, delta.VerbDelete:
		return delta.Op{Verb: op.Verb, Path: at(op.Path), Value: op.Value}
	case delta.VerbAppend:
		return delta.Op{Verb: op.Verb, Path: at(op.Path), Text: op.Text}
	case delta.VerbTruncate:
		return delta.Op{Verb: op.Verb, Path: at(op.Path), Count: op.Count}
	default:
		return op
	}
}

func snapshotObservers(mount *viewMount) []ViewObserver {
	observers := make([]ViewObserver, 0, len(mount.observers))
	for _, observer := range mount.observers {
		observers = append(observers, observer)
	}
	return observers
}
