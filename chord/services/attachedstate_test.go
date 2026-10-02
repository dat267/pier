package services

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dat267/pier/chord"
	"github.com/dat267/pier/chord/delta"
)

// Port of the "authoritative replicated state sources" cases of upstream
// packages/chord/test/state.test.ts.

type sourceValue struct {
	Value int `json:"value"`
}

// The attached state is a read-only replicated state.
var _ chord.ReplicatedState[sourceValue] = (*AttachedState[sourceValue])(nil)

// testAttachment is the upstream TestSourceAttachment: it buffers frames until
// activation and then forwards them.
type testAttachment struct {
	mu        sync.Mutex
	value     sourceValue
	cursor    int
	onDispose func()
	buffer    []SourceFrame[sourceValue]
	listener  func(SourceFrame[sourceValue])
	activated bool
	disposed  bool
}

func (a *testAttachment) Snapshot() (sourceValue, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.value, a.cursor
}

func (a *testAttachment) Activate(listener func(SourceFrame[sourceValue])) {
	a.mu.Lock()
	if a.activated {
		a.mu.Unlock()
		panic("attachment is already active")
	}
	a.activated = true
	a.listener = listener
	buffered := a.buffer
	a.buffer = nil
	a.mu.Unlock()
	for _, frame := range buffered {
		listener(frame)
	}
}

func (a *testAttachment) publish(frame SourceFrame[sourceValue]) {
	a.mu.Lock()
	if a.disposed {
		a.mu.Unlock()
		return
	}
	listener := a.listener
	if listener == nil {
		a.buffer = append(a.buffer, frame)
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	listener(frame)
}

func (a *testAttachment) Dispose() {
	a.mu.Lock()
	if a.disposed {
		a.mu.Unlock()
		return
	}
	a.disposed = true
	a.buffer = nil
	onDispose := a.onDispose
	a.mu.Unlock()
	if onDispose != nil {
		onDispose()
	}
}

type testSource struct {
	mu          sync.Mutex
	value       sourceValue
	cursor      int
	attachments map[*testAttachment]struct{}
	onAttach    func()
}

func newTestSource(value sourceValue, cursor ...int) *testSource {
	source := &testSource{value: value, attachments: map[*testAttachment]struct{}{}}
	if len(cursor) > 0 {
		source.cursor = cursor[0]
	}
	return source
}

func (s *testSource) Attach() SourceAttachment[sourceValue] {
	s.mu.Lock()
	attachment := &testAttachment{value: s.value, cursor: s.cursor}
	attachment.onDispose = func() {
		s.mu.Lock()
		delete(s.attachments, attachment)
		s.mu.Unlock()
	}
	s.attachments[attachment] = struct{}{}
	onAttach := s.onAttach
	s.mu.Unlock()
	if onAttach != nil {
		onAttach()
	}
	return attachment
}

func (s *testSource) commit(value sourceValue, ops []delta.Op, cursor int) {
	s.mu.Lock()
	s.value = value
	s.cursor = cursor
	attachments := make([]*testAttachment, 0, len(s.attachments))
	for attachment := range s.attachments {
		attachments = append(attachments, attachment)
	}
	s.mu.Unlock()
	frame := SourceFrame[sourceValue]{Cursor: cursor, Value: value, Ops: ops, Context: context.Background()}
	for _, attachment := range attachments {
		attachment.publish(frame)
	}
}

func setOp(value int) []delta.Op {
	return []delta.Op{{Verb: delta.VerbSet, Path: delta.Path{"value"}, Value: float64(value)}}
}

// TestAttachedStateDrainsQueuedCommits covers upstream's "captures before
// activation and drains queued commits in order".
func TestAttachedStateDrainsQueuedCommits(t *testing.T) {
	initial := sourceValue{0}
	first := sourceValue{1}
	second := sourceValue{2}
	source := newTestSource(initial, 10)
	source.onAttach = func() {
		source.commit(first, setOp(1), 11)
		source.commit(second, setOp(2), 12)
	}
	state := NewAttachedState[sourceValue](source, nil)
	type delivery struct {
		value    sourceValue
		sequence int
	}
	var deliveries []delivery
	state.Subscribe(func(value sourceValue, _ chord.Context, info chord.ReplicatedStateDelivery) {
		deliveries = append(deliveries, delivery{value, info.Sequence})
	})
	value, _ := state.Value()
	if value != second {
		t.Fatalf("value = %+v", value)
	}
	if len(deliveries) != 1 || deliveries[0].value != second || deliveries[0].sequence != 2 {
		t.Fatalf("deliveries = %+v", deliveries)
	}
}

// TestAttachedStateHydratesAtZero covers "hydrates at sequence zero when
// attaching after existing commits".
func TestAttachedStateHydratesAtZero(t *testing.T) {
	source := newTestSource(sourceValue{0}, 40)
	current := sourceValue{1}
	source.commit(current, setOp(1), 41)
	state := NewAttachedState[sourceValue](source, nil)
	var sequences []int
	state.Subscribe(func(_ sourceValue, _ chord.Context, info chord.ReplicatedStateDelivery) {
		sequences = append(sequences, info.Sequence)
	})
	value, _ := state.Value()
	if value != current || len(sequences) != 1 || sequences[0] != 0 {
		t.Fatalf("value = %+v sequences = %v", value, sequences)
	}
}

// TestAttachedStatePublishesExactReferences covers "publishes exact source
// value and operation references without applying or re-diffing".
func TestAttachedStatePublishesExactReferences(t *testing.T) {
	source := newTestSource(sourceValue{0})
	state := NewAttachedState[sourceValue](source, nil)
	next := sourceValue{1}
	ops := setOp(1)
	var publishedOps []delta.Op
	state.SubscribeOps(func(received []delta.Op, _ int, _ chord.Context) error {
		publishedOps = received
		return nil
	})
	var publishedValue *sourceValue
	state.Subscribe(func(value sourceValue, _ chord.Context, info chord.ReplicatedStateDelivery) {
		if info.Kind == chord.DeliveryUpdate {
			copied := value
			publishedValue = &copied
		}
	})
	source.commit(next, ops, 1)
	value, _ := state.Value()
	if value != next {
		t.Fatalf("value = %+v", value)
	}
	if publishedValue == nil || *publishedValue != next {
		t.Fatalf("published value = %+v", publishedValue)
	}
	if len(publishedOps) != len(ops) || &publishedOps[0] != &ops[0] {
		t.Fatalf("ops = %+v", publishedOps)
	}
}

// TestAttachedStateBuffersReentrantFrames covers "buffers reentrant frames and
// skips updates covered by a late hydration".
func TestAttachedStateBuffersReentrantFrames(t *testing.T) {
	source := newTestSource(sourceValue{0})
	state := NewAttachedState[sourceValue](source, nil)
	var received []int
	type lateDelivery struct {
		kind     string
		sequence int
		value    int
	}
	var late []lateDelivery
	nested := false
	state.SubscribeOps(func(_ []delta.Op, sequence int, _ chord.Context) error {
		if sequence != 1 || nested {
			return nil
		}
		nested = true
		source.commit(sourceValue{2}, setOp(2), 2)
		state.Subscribe(func(value sourceValue, _ chord.Context, info chord.ReplicatedStateDelivery) {
			late = append(late, lateDelivery{info.Kind, info.Sequence, value.Value})
		})
		return nil
	})
	state.Subscribe(func(value sourceValue, _ chord.Context, info chord.ReplicatedStateDelivery) {
		if info.Kind == chord.DeliveryUpdate {
			received = append(received, value.Value)
		}
	})
	source.commit(sourceValue{1}, setOp(1), 1)
	if !reflect.DeepEqual(received, []int{1, 2}) {
		t.Fatalf("received = %v", received)
	}
	want := []lateDelivery{{chord.DeliveryHydrate, 2, 2}}
	if !reflect.DeepEqual(late, want) {
		t.Fatalf("late = %+v", late)
	}
}

// TestAttachedStateReportsListenerFailures covers "reports listener failures
// without throwing them into the source".
func TestAttachedStateReportsListenerFailures(t *testing.T) {
	source := newTestSource(sourceValue{0})
	var errors []error
	state := NewAttachedState[sourceValue](source, &SourceOptions{OnError: func(err error) {
		errors = append(errors, err)
	}})
	var received []int
	state.Subscribe(func(_ sourceValue, _ chord.Context, info chord.ReplicatedStateDelivery) {
		if info.Kind == chord.DeliveryUpdate {
			panic(fmt.Errorf("listener failed"))
		}
	})
	state.Subscribe(func(value sourceValue, _ chord.Context, info chord.ReplicatedStateDelivery) {
		if info.Kind == chord.DeliveryUpdate {
			received = append(received, value.Value)
		}
	})
	source.commit(sourceValue{1}, setOp(1), 1)
	source.commit(sourceValue{2}, setOp(2), 2)
	if !reflect.DeepEqual(received, []int{1, 2}) {
		t.Fatalf("received = %v", received)
	}
	if len(errors) != 2 || errors[0].Error() != "listener failed" {
		t.Fatalf("errors = %v", errors)
	}
}

// TestAttachedStateReportsCursorGaps covers "reports cursor gaps, disposes the
// broken attachment, and ignores later frames".
func TestAttachedStateReportsCursorGaps(t *testing.T) {
	source := newTestSource(sourceValue{0}, 5)
	var errors []error
	state := NewAttachedState[sourceValue](source, &SourceOptions{OnError: func(err error) {
		errors = append(errors, err)
	}})
	source.commit(sourceValue{2}, setOp(2), 7)
	if len(errors) == 0 || !strings.Contains(errors[0].Error(), "expected 6, received 7") {
		t.Fatalf("errors = %v", errors)
	}
	if len(source.attachments) != 0 {
		t.Fatalf("attachments = %d", len(source.attachments))
	}
	if value, _ := state.Value(); value != (sourceValue{0}) {
		t.Fatalf("value = %+v", value)
	}
	source.commit(sourceValue{3}, setOp(3), 8)
	if value, _ := state.Value(); value != (sourceValue{0}) {
		t.Fatalf("value after gap = %+v", value)
	}
}

// TestAttachedStateAttachmentsIndependent covers "keeps attachments
// independent and disposes each idempotently".
func TestAttachedStateAttachmentsIndependent(t *testing.T) {
	source := newTestSource(sourceValue{0})
	first := NewAttachedState[sourceValue](source, nil)
	second := NewAttachedState[sourceValue](source, nil)
	if len(source.attachments) != 2 {
		t.Fatalf("attachments = %d", len(source.attachments))
	}
	source.commit(sourceValue{1}, setOp(1), 1)
	if value, _ := first.Value(); value.Value != 1 {
		t.Fatalf("first = %+v", value)
	}
	if value, _ := second.Value(); value.Value != 1 {
		t.Fatalf("second = %+v", value)
	}
	first.Dispose()
	first.Dispose()
	if len(source.attachments) != 1 {
		t.Fatalf("attachments after dispose = %d", len(source.attachments))
	}
	second.Dispose()
	if len(source.attachments) != 0 {
		t.Fatalf("attachments after both = %d", len(source.attachments))
	}
}
