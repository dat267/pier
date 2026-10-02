package services

import (
	"context"
	"fmt"
	"sync"

	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
)

// Port of the authoritative-state-source half of src/services/state.ts: the
// attachment-based "attached" replicated state and the source contract it
// consumes (upstream ReplicatedStateSource family).

// SourceFrame is one immutable authoritative revision committed after an
// attachment snapshot (upstream ReplicatedStateSourceFrame).
type SourceFrame[T any] struct {
	// Cursor is the monotonic source cursor; the first frame after a snapshot
	// must be the snapshot cursor plus one.
	Cursor int
	// Value is the exact immutable value produced by this commit.
	Value T
	// Ops is the exact immutable batch that produced Value from the preceding
	// revision.
	Ops []delta.Op
	// Context carries the commit context.
	Context chord.Context
}

// SourceAttachment is one attachment to an authoritative source (upstream
// ReplicatedStateSourceAttachment). Snapshot is captured atomically by Attach;
// Activate installs the sole listener and synchronously drains every buffered
// frame in commit order.
type SourceAttachment[T any] interface {
	Snapshot() (value T, cursor int)
	Activate(listener func(frame SourceFrame[T]))
	Dispose()
}

// StateSource is an authoritative immutable revision source (upstream
// ReplicatedStateSource).
type StateSource[T any] interface {
	Attach() SourceAttachment[T]
}

// SourceOptions configures an attached state (upstream
// ReplicatedStateSourceOptions).
type SourceOptions struct {
	// OnError receives source-contract and publication-listener failures
	// without throwing them into the source.
	OnError func(error)
}

// AttachedState is a synchronously hydrated publication-only state backed by
// one source attachment (upstream AttachedReplicatedState).
//
// D187: upstream serializes per-subscription deliveries behind asynchronous
// callbacks (a 100-frame pending window with newest-wins overflow); the port's
// service states deliver synchronously, so a listener that subscribes mid-
// publication is deferred until the current publication drain finishes and
// then hydrates at the then-current sequence, which preserves upstream's
// "a late hydration skips updates it covers" behavior.
type AttachedState[T any] struct {
	mu         sync.Mutex
	value      T
	hasValue   bool
	cursor     int
	sequence   int
	listeners  map[int]func(value T, ctx chord.Context, delivery chord.ReplicatedStateDelivery)
	sources    map[int]func(ops []delta.Op, sequence int, ctx chord.Context) error
	pending    []func()
	nextID     int
	attachment SourceAttachment[T]
	disposed   bool
	draining   bool
	onError    func(error)
}

// NewAttachedState attaches to a source, captures its snapshot and drains the
// frames committed before and during activation (upstream replicatedState(source)).
func NewAttachedState[T any](source StateSource[T], options *SourceOptions) *AttachedState[T] {
	report := func(error) {}
	if options != nil && options.OnError != nil {
		report = options.OnError
	}
	state := &AttachedState[T]{
		listeners: map[int]func(value T, ctx chord.Context, delivery chord.ReplicatedStateDelivery){},
		sources:   map[int]func(ops []delta.Op, sequence int, ctx chord.Context) error{},
		onError:   report,
	}
	attachment := source.Attach()
	state.attachment = attachment
	state.value, state.cursor = attachment.Snapshot()
	state.hasValue = true
	attachment.Activate(state.receive)
	return state
}

// Value is the current immutable value (upstream `value`).
func (s *AttachedState[T]) Value() (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, s.hasValue
}

// Published is the current immutable value (the port's ReplicatedStateSource
// view; always present for an attached state).
func (s *AttachedState[T]) Published() T {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value
}

// Sequence is the number of accepted frames (0 before the first commit).
func (s *AttachedState[T]) Sequence() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sequence
}

// Subscribe registers a value listener and hydrates it synchronously. The
// returned function unsubscribes.
func (s *AttachedState[T]) Subscribe(listener func(value T, ctx chord.Context, delivery chord.ReplicatedStateDelivery)) func() {
	id := func() int {
		s.mu.Lock()
		defer s.mu.Unlock()
		next := s.nextID
		s.nextID++
		return next
	}()
	register := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.disposed {
			return false
		}
		s.listeners[id] = listener
		return true
	}
	hydrate := func() {
		s.mu.Lock()
		if s.disposed {
			s.mu.Unlock()
			return
		}
		value, sequence := s.value, s.sequence
		s.mu.Unlock()
		listener(value, context.Background(), chord.ReplicatedStateDelivery{Kind: chord.DeliveryHydrate, Sequence: sequence})
	}
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return func() {}
	}
	if s.draining {
		// A subscription made while a publication is being delivered hydrates
		// after the drain, at the sequence that drain reaches, and the updates
		// the hydration covers stay hidden (upstream's late-hydration rule).
		s.pending = append(s.pending, func() {
			if register() {
				hydrate()
			}
		})
		s.mu.Unlock()
	} else {
		s.mu.Unlock()
		if register() {
			hydrate()
		}
	}
	return func() {
		s.mu.Lock()
		delete(s.listeners, id)
		s.mu.Unlock()
	}
}

// SubscribeOps observes accepted op batches (the port's internal source view).
func (s *AttachedState[T]) SubscribeOps(listener func(ops []delta.Op, sequence int, ctx chord.Context) error) func() {
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	s.sources[id] = listener
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.sources, id)
		s.mu.Unlock()
	}
}

// Dispose releases the source attachment; the last published value stays
// readable (upstream AttachedReplicatedState.dispose).
func (s *AttachedState[T]) Dispose() {
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return
	}
	s.disposed = true
	attachment := s.attachment
	s.attachment = nil
	s.listeners = map[int]func(value T, ctx chord.Context, delivery chord.ReplicatedStateDelivery){}
	s.sources = map[int]func(ops []delta.Op, sequence int, ctx chord.Context) error{}
	s.mu.Unlock()
	if attachment != nil {
		attachment.Dispose()
	}
}

// receive accepts one committed frame, validating the cursor sequence.
func (s *AttachedState[T]) receive(frame SourceFrame[T]) {
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return
	}
	expected := s.cursor + 1
	if frame.Cursor != expected {
		s.mu.Unlock()
		s.onError(fmt.Errorf("Replicated state source cursor has a gap: expected %d, received %d", expected, frame.Cursor))
		s.Dispose()
		return
	}
	s.mu.Unlock()
	s.enqueue(frame)
}

// enqueue delivers one frame, deferring reentrant commits so they follow in
// commit order (upstream buffers reentrant frames).
func (s *AttachedState[T]) enqueue(frame SourceFrame[T]) {
	s.mu.Lock()
	s.pending = append(s.pending, func() { s.publish(frame) })
	if s.draining {
		s.mu.Unlock()
		return
	}
	s.draining = true
	s.mu.Unlock()
	s.drain()
}

func (s *AttachedState[T]) drain() {
	for {
		s.mu.Lock()
		if len(s.pending) == 0 || s.disposed {
			s.pending = nil
			s.draining = false
			s.mu.Unlock()
			return
		}
		next := s.pending[0]
		s.pending = s.pending[1:]
		s.mu.Unlock()
		next()
	}
}

// publish commits one frame, then delivers it to the current listeners.
func (s *AttachedState[T]) publish(frame SourceFrame[T]) {
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return
	}
	s.value = frame.Value
	s.cursor = frame.Cursor
	s.sequence++
	sequence := s.sequence
	value := s.value
	listeners := make([]func(value T, ctx chord.Context, delivery chord.ReplicatedStateDelivery), 0, len(s.listeners))
	for _, listener := range s.listeners {
		listeners = append(listeners, listener)
	}
	sourceListeners := make([]func(ops []delta.Op, sequence int, ctx chord.Context) error, 0, len(s.sources))
	for _, listener := range s.sources {
		sourceListeners = append(sourceListeners, listener)
	}
	s.mu.Unlock()

	ctx := frame.Context
	if ctx == nil {
		ctx = context.Background()
	}
	for _, listener := range sourceListeners {
		if err := listener(frame.Ops, sequence, ctx); err != nil {
			s.onError(err)
		}
	}
	delivery := chord.ReplicatedStateDelivery{Kind: chord.DeliveryUpdate, Sequence: sequence}
	for _, listener := range listeners {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					s.onError(asError(recovered))
				}
			}()
			listener(value, ctx, delivery)
		}()
	}
}

func asError(value any) error {
	if err, ok := value.(error); ok {
		return err
	}
	return fmt.Errorf("%v", value)
}
