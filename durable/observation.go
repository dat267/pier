package durable

import (
	"context"
	"reflect"
	"sync"

	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
	"github.com/dat267/pier/chord/services"
)

// Port of session/observation.ts: the session-to-Chord bridge. A
// CommittedStateSource is owned one-to-one by one attached state (a document or
// a conversation view) and a CommittedWatch is the serialized exact-frame
// observation of one document incarnation.

// WatchEnd reasons (upstream WatchEnd).
const (
	WatchReasonStopped       = "stopped"
	WatchReasonCancelled     = "cancelled"
	WatchReasonSessionClosed = "session_closed"
	WatchReasonRetired       = "retired"
	WatchReasonListenerError = "listener_error"
)

// WatchEnd is a watch's terminal result (upstream WatchEnd).
type WatchEnd struct {
	Reason string
	// Err is set for WatchReasonListenerError.
	Err error
}

// WatchListener observes one watch frame. An error terminates the watch with
// WatchReasonListenerError.
type WatchListener[T any] func(value T, ops []delta.Op, ctx chord.Context) error

// WatchHandle is the serialized exact-frame observation contract (upstream
// WatchHandle). Closed settles when the watch terminates; End is the terminal
// result it carries.
type WatchHandle[T any] interface {
	// Value is the acquisition revision before start and the latest delivered
	// immutable revision afterwards.
	Value() T
	// Start installs the sole listener; it never invokes it inline.
	Start(listener WatchListener[T])
	// Stop idempotently stops future callbacks and returns the terminal
	// result.
	Stop() WatchEnd
	// Closed settles when the watch terminates.
	Closed() <-chan struct{}
	// End is the terminal result, available once Closed has settled.
	End() (WatchEnd, bool)
}

// DocumentWatch observes a document incarnation, null after retirement
// (upstream DocumentWatch).
type DocumentWatch[T any] = WatchHandle[T]

// maxPendingWatchFrames bounds the frames retained behind an unavailable watch
// listener (upstream MAX_PENDING_WATCH_FRAMES).
const maxPendingWatchFrames = 100

// RetirementOperations is the canonical terminal update for a retired
// document incarnation (upstream RETIREMENT_OPERATIONS).
var RetirementOperations = []delta.Op{{Verb: delta.VerbReplace, Value: nil}}

// CommittedStateSource is the session-to-Chord bridge owned one-to-one by one
// attached state. A null value retires it (upstream CommittedStateSource).
type CommittedStateSource[T any] struct {
	mu          sync.Mutex
	attachments map[*committedAttachment[T]]struct{}
	release     func()
	value       T
	cursor      int
	retired     bool
	closed      bool
}

// NewCommittedStateSource builds a source over an initial value; release runs
// once after the last attachment is disposed (upstream constructor).
func NewCommittedStateSource[T any](value T, release func()) *CommittedStateSource[T] {
	return &CommittedStateSource[T]{value: value, release: release}
}

// Attach captures a snapshot and registers the attachment (upstream attach).
func (s *CommittedStateSource[T]) Attach() services.SourceAttachment[T] {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		panic("State source is closed")
	}
	if s.attachments == nil {
		s.attachments = map[*committedAttachment[T]]struct{}{}
	}
	attachment := &committedAttachment[T]{}
	attachment.snapshotValue = s.value
	attachment.snapshotCursor = s.cursor
	attachment.release = func() {
		s.mu.Lock()
		delete(s.attachments, attachment)
		empty := len(s.attachments) == 0
		s.mu.Unlock()
		if empty {
			s.finishDisposal()
		}
	}
	s.attachments[attachment] = struct{}{}
	return attachment
}

// Advance publishes one exact committed frame. A nil value retires the source
// (upstream advance).
func (s *CommittedStateSource[T]) Advance(value T, ops []delta.Op, ctx chord.Context) {
	s.mu.Lock()
	if s.closed || s.retired {
		s.mu.Unlock()
		return
	}
	s.value = value
	s.cursor++
	if isNilValue(value) {
		s.retired = true
	}
	frame := services.SourceFrame[T]{Cursor: s.cursor, Value: value, Ops: ops, Context: ctx}
	attachments := make([]*committedAttachment[T], 0, len(s.attachments))
	for attachment := range s.attachments {
		attachments = append(attachments, attachment)
	}
	s.mu.Unlock()
	for _, attachment := range attachments {
		attachment.publish(frame)
	}
}

// CloseSession disposes every attachment and releases the source (upstream
// closeSession).
func (s *CommittedStateSource[T]) CloseSession() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	attachments := make([]*committedAttachment[T], 0, len(s.attachments))
	for attachment := range s.attachments {
		attachments = append(attachments, attachment)
	}
	s.mu.Unlock()
	for _, attachment := range attachments {
		attachment.Dispose()
	}
	s.finishDisposal()
}

// finishDisposal marks the source closed and runs the release exactly once.
func (s *CommittedStateSource[T]) finishDisposal() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	var zero T
	s.value = zero
	release := s.release
	s.release = nil
	s.mu.Unlock()
	if release != nil {
		release()
	}
}

// isNilValue reports whether a generic value is nil, including a typed nil map
// or pointer (the retired-document sentinel).
func isNilValue[T any](value T) bool {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Map, reflect.Pointer, reflect.Slice, reflect.Interface, reflect.Chan, reflect.Func:
		return reflected.IsNil()
	}
	return false
}

// committedAttachment is one attachment to a CommittedStateSource (upstream
// SessionSourceAttachment).
type committedAttachment[T any] struct {
	mu             sync.Mutex
	snapshotValue  T
	snapshotCursor int
	release        func()
	listener       func(services.SourceFrame[T])
	activated      bool
	disposed       bool
}

func (a *committedAttachment[T]) Snapshot() (T, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.snapshotValue, a.snapshotCursor
}

func (a *committedAttachment[T]) Activate(listener func(services.SourceFrame[T])) {
	a.mu.Lock()
	if a.activated {
		a.mu.Unlock()
		panic("State attachment is already active")
	}
	if a.disposed {
		a.mu.Unlock()
		panic("State attachment is disposed")
	}
	a.activated = true
	a.listener = listener
	a.mu.Unlock()
}

func (a *committedAttachment[T]) publish(frame services.SourceFrame[T]) {
	a.mu.Lock()
	if a.disposed {
		a.mu.Unlock()
		return
	}
	listener := a.listener
	a.mu.Unlock()
	if listener != nil {
		listener(frame)
	}
}

func (a *committedAttachment[T]) Dispose() {
	a.mu.Lock()
	if a.disposed {
		a.mu.Unlock()
		return
	}
	a.disposed = true
	a.listener = nil
	release := a.release
	a.release = nil
	a.mu.Unlock()
	if release != nil {
		release()
	}
}

// CommittedWatch is a serialized exact-frame watch bound to one document
// incarnation or conversation view (upstream CommittedWatch).
type CommittedWatch[T any] struct {
	mu       sync.Mutex
	value    T
	detach   func()
	replace  func() T
	closed   chan struct{}
	end      *WatchEnd
	pending  []watchFrame[T]
	listener WatchListener[T]
	started  bool
	running  bool
	retired  bool
	settled  bool
	detached bool
	// cancel observes the context installed by ObserveCancellation.
	cancel context.Context
	// cancelStop releases the cancellation goroutine.
	cancelStop chan struct{}
}

type watchFrame[T any] struct {
	value T
	ops   []delta.Op
	ctx   chord.Context
}

// NewCommittedWatch builds a watch over an acquisition revision. replace, when
// set, is the value an overflow delivers; the newest value otherwise (upstream
// constructor).
func NewCommittedWatch[T any](value T, detach func(), replace func() T) *CommittedWatch[T] {
	return &CommittedWatch[T]{value: value, detach: detach, replace: replace, closed: make(chan struct{})}
}

// Value is the acquisition revision before start and the latest delivered
// revision afterwards.
func (w *CommittedWatch[T]) Value() T {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.value
}

// Closed settles when the watch terminates.
func (w *CommittedWatch[T]) Closed() <-chan struct{} {
	return w.closed
}

// End is the terminal result, available once Closed has settled.
func (w *CommittedWatch[T]) End() (WatchEnd, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.end == nil {
		return WatchEnd{}, false
	}
	return *w.end, true
}

// Start installs the sole listener (upstream start).
func (w *CommittedWatch[T]) Start(listener WatchListener[T]) {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		panic("Watch is already started")
	}
	if w.end != nil {
		w.mu.Unlock()
		panic("Watch is stopped")
	}
	w.started = true
	w.listener = listener
	start := len(w.pending) > 0
	if start {
		w.running = true
	}
	w.mu.Unlock()
	if start {
		go w.drain()
	}
}

// Stop idempotently ends the watch with reason stopped (upstream stop).
func (w *CommittedWatch[T]) Stop() WatchEnd {
	w.terminate(WatchEnd{Reason: WatchReasonStopped})
	<-w.closed
	end, _ := w.End()
	return end
}

// ObserveCancellation cancels the watch when ctx is cancelled (upstream
// observeCancellation).
func (w *CommittedWatch[T]) ObserveCancellation(ctx context.Context) {
	w.mu.Lock()
	if w.cancel != nil {
		w.mu.Unlock()
		panic("Watch cancellation is already installed")
	}
	if w.end != nil {
		w.mu.Unlock()
		return
	}
	w.cancel = ctx
	w.cancelStop = make(chan struct{})
	stop := w.cancelStop
	w.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
			w.Cancel()
		case <-stop:
		}
	}()
}

// Cancel ends the watch with reason cancelled (upstream cancel).
func (w *CommittedWatch[T]) Cancel() { w.terminate(WatchEnd{Reason: WatchReasonCancelled}) }

// CloseSession ends the watch with reason session_closed (upstream
// closeSession).
func (w *CommittedWatch[T]) CloseSession() {
	w.terminate(WatchEnd{Reason: WatchReasonSessionClosed})
}

// Advance queues one exact frame; a nil value retires the watch (upstream
// advance).
func (w *CommittedWatch[T]) Advance(value T, ops []delta.Op, ctx chord.Context) {
	w.mu.Lock()
	if w.end != nil || w.retired {
		w.mu.Unlock()
		return
	}
	if isNilValue(value) {
		w.retired = true
	}
	if len(w.pending) >= maxPendingWatchFrames {
		w.pending = nil
		replacement := value
		if w.replace != nil {
			replacement = w.replace()
		}
		w.pending = append(w.pending, watchFrame[T]{
			value: replacement,
			ops:   []delta.Op{{Verb: delta.VerbReplace, Value: any(replacement)}},
			ctx:   ctx,
		})
	} else {
		w.pending = append(w.pending, watchFrame[T]{value: value, ops: ops, ctx: ctx})
	}
	start := w.started && !w.running && w.end == nil
	if start {
		w.running = true
	}
	w.mu.Unlock()
	if start {
		go w.drain()
	}
}

// drain delivers queued frames in order until the queue is empty.
func (w *CommittedWatch[T]) drain() {
	for {
		w.mu.Lock()
		if w.end != nil || len(w.pending) == 0 || !w.started {
			w.running = false
			w.mu.Unlock()
			return
		}
		frame := w.pending[0]
		w.pending = w.pending[1:]
		w.value = frame.value
		listener := w.listener
		w.mu.Unlock()

		ctx := frame.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		if err := listener(frame.value, frame.ops, context.WithoutCancel(ctx)); err != nil {
			w.terminate(WatchEnd{Reason: WatchReasonListenerError, Err: err})
			return
		}
		if isNilValue(frame.value) {
			w.terminate(WatchEnd{Reason: WatchReasonRetired})
			return
		}
	}
}

// terminate settles the watch once (upstream terminate).
func (w *CommittedWatch[T]) terminate(end WatchEnd) {
	w.mu.Lock()
	if w.end != nil {
		w.mu.Unlock()
		return
	}
	w.end = &end
	w.pending = nil
	alreadyDetached := w.detached
	w.detached = true
	detach := w.detach
	stop := w.cancelStop
	w.mu.Unlock()
	if !alreadyDetached && detach != nil {
		detach()
	}
	if stop != nil {
		close(stop)
	}
	close(w.closed)
}
