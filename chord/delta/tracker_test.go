package delta

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dat267/pier/chord"
)

// Port of the lifecycle cases of upstream
// packages/chord/test/delta-tracker/tracker.test.ts. Upstream's Proxy draft
// becomes an explicit mutable draft (D11); the batch shape is not contractual,
// so the tests verify that applying the batch to the base reproduces the
// candidate (as the upstream README requires).

func trackerJSONEqual(t *testing.T, left, right chord.JsonValue) bool {
	t.Helper()
	ops, err := Diff(left, right, nil)
	if err != nil {
		t.Fatal(err)
	}
	return len(ops) == 0
}

func mustPrepare(t *testing.T, change *Change) *Prepared {
	t.Helper()
	prepared, err := change.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func mustAdopt(t *testing.T, tracker *Tracker, prepared *Prepared) {
	t.Helper()
	if err := tracker.Adopt(prepared); err != nil {
		t.Fatal(err)
	}
}

func mapPointer(value chord.JsonValue) uintptr {
	return reflect.ValueOf(value).Pointer()
}

// TestTrackerMaterializesAndAdopts covers the upstream pointer-swap case: the
// base is never mutated, the candidate is a new revision, and the batch
// reproduces it.
func TestTrackerMaterializesAndAdopts(t *testing.T) {
	initial := map[string]any{"count": 1, "nested": map[string]any{"text": "a"}, "values": []any{1}}
	tracker := Track(initial)
	if tracker.Value() == nil {
		t.Fatal("missing value")
	}
	change := tracker.BeginChange()
	draft := change.State().(map[string]any)
	draft["count"] = 2
	draft["nested"].(map[string]any)["text"] = "ab"
	draft["values"] = append(draft["values"].([]any), 2)

	// The base revision is untouched.
	if !trackerJSONEqual(t, initial, map[string]any{"count": 1, "nested": map[string]any{"text": "a"}, "values": []any{1}}) {
		t.Fatalf("base mutated: %+v", initial)
	}
	prepared := mustPrepare(t, change)
	if !trackerJSONEqual(t, prepared.Base(), initial) {
		t.Fatalf("base = %+v", prepared.Base())
	}
	next := prepared.Value()
	if mapPointer(next) == mapPointer(initial) {
		t.Fatal("candidate aliases the base")
	}
	if !trackerJSONEqual(t, next, map[string]any{"count": 2, "nested": map[string]any{"text": "ab"}, "values": []any{1, 2}}) {
		t.Fatalf("candidate = %+v", next)
	}
	// The batch transforms the base into the candidate.
	replayed, err := ApplyImmutable(prepared.Base(), prepared.Ops())
	if err != nil {
		t.Fatal(err)
	}
	if !trackerJSONEqual(t, replayed, next) {
		t.Fatalf("replayed = %+v, want %+v", replayed, next)
	}
	if change.Settled() != true {
		t.Fatal("change not settled after prepare")
	}
	// The draft is not usable after prepare.
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("draft stayed usable after prepare")
			}
		}()
		_ = change.State()
	}()

	// Adoption swaps the pointer; the old revision is unchanged.
	mustAdopt(t, tracker, prepared)
	if mapPointer(tracker.Value()) != mapPointer(next) {
		t.Fatal("adoption did not swap the pointer")
	}
	if tracker.Revision() != 1 {
		t.Fatalf("revision = %d", tracker.Revision())
	}
	if !trackerJSONEqual(t, initial, map[string]any{"count": 1, "nested": map[string]any{"text": "a"}, "values": []any{1}}) {
		t.Fatalf("adoption mutated the previous revision: %+v", initial)
	}
}

// TestTrackerAbort covers idempotent aborts and settled changes.
func TestTrackerAbort(t *testing.T) {
	tracker := Track(map[string]any{"child": map[string]any{"value": 1}})
	change := tracker.BeginChange()
	change.State().(map[string]any)["child"].(map[string]any)["value"] = 2
	change.Abort()
	change.Abort()
	if value := tracker.Value().(map[string]any)["child"].(map[string]any)["value"]; value != 1 {
		t.Fatalf("value = %v", value)
	}
	if _, err := change.Prepare(); err == nil || !strings.Contains(err.Error(), "settled") {
		t.Fatalf("prepare after abort = %v", err)
	}
}

// TestTrackerCompetingChanges covers staleness: adopting one preparation
// stales the others and consumes itself.
func TestTrackerCompetingChanges(t *testing.T) {
	tracker := Track(map[string]any{"value": 0})
	first := tracker.BeginChange()
	second := tracker.BeginChange()
	first.State().(map[string]any)["value"] = 1
	second.State().(map[string]any)["value"] = 2
	firstPrepared := mustPrepare(t, first)
	secondPrepared := mustPrepare(t, second)
	held := secondPrepared.Value()

	mustAdopt(t, tracker, firstPrepared)
	if value := tracker.Value().(map[string]any)["value"]; value != 1 {
		t.Fatalf("value = %v", value)
	}
	if value := held.(map[string]any)["value"]; value != 2 {
		t.Fatalf("held candidate = %v", value)
	}
	if err := tracker.Adopt(secondPrepared); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale adopt = %v", err)
	}
	if err := tracker.Adopt(firstPrepared); err == nil || !strings.Contains(err.Error(), "already adopted") {
		t.Fatalf("double adopt = %v", err)
	}
}

// TestTrackerAbortedAndStaleCandidatesReadable covers the readability of
// aborted and stale preparations.
func TestTrackerAbortedAndStaleCandidatesReadable(t *testing.T) {
	tracker := Track(map[string]any{"value": 0})
	abortedChange := tracker.BeginChange()
	abortedChange.State().(map[string]any)["value"] = 1
	aborted := mustPrepare(t, abortedChange)
	abortedView := aborted.Value()
	aborted.Abort()
	if value := abortedView.(map[string]any)["value"]; value != 1 {
		t.Fatalf("aborted view = %v", value)
	}
	if value := aborted.Value().(map[string]any)["value"]; value != 1 {
		t.Fatalf("aborted candidate = %v", value)
	}

	loserChange := tracker.BeginChange()
	loserChange.State().(map[string]any)["value"] = 2
	loser := mustPrepare(t, loserChange)
	loserView := loser.Value()
	winner, err := tracker.PrepareReplace(map[string]any{"value": 3})
	if err != nil {
		t.Fatal(err)
	}
	mustAdopt(t, tracker, winner)
	if value := loserView.(map[string]any)["value"]; value != 2 {
		t.Fatalf("loser view = %v", value)
	}
	if value := loser.Value().(map[string]any)["value"]; value != 2 {
		t.Fatalf("loser candidate = %v", value)
	}
}

// TestTrackerSettledChangeAbortsPrepared covers upstream's "a settled Change
// aborts its prepared result".
func TestTrackerSettledChangeAbortsPrepared(t *testing.T) {
	tracker := Track(map[string]any{"value": 0, "nested": map[string]any{"value": 1}})
	change := tracker.BeginChange()
	change.State().(map[string]any)["value"] = 1
	prepared := mustPrepare(t, change)
	operations := prepared.Ops()
	change.Abort()
	change.Abort()
	if len(prepared.Ops()) != len(operations) {
		t.Fatalf("ops changed after abort: %v -> %v", operations, prepared.Ops())
	}
	if !trackerJSONEqual(t, prepared.Value(), map[string]any{"value": 1, "nested": map[string]any{"value": 1}}) {
		t.Fatalf("candidate = %+v", prepared.Value())
	}
	if err := tracker.Adopt(prepared); err == nil || !strings.Contains(err.Error(), "aborted") {
		t.Fatalf("adopt = %v", err)
	}
}

// TestTrackerRejectsForeignAndAborted covers the foreign tracker and aborted
// adoption paths.
func TestTrackerRejectsForeignAndAborted(t *testing.T) {
	first := Track(map[string]any{"value": 0})
	second := Track(map[string]any{"value": 0})
	prepared, err := first.PrepareReplace(map[string]any{"value": 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Adopt(prepared); err == nil || !strings.Contains(err.Error(), "another tracker") {
		t.Fatalf("foreign adopt = %v", err)
	}
	prepared.Abort()
	prepared.Abort()
	if err := first.Adopt(prepared); err == nil || !strings.Contains(err.Error(), "aborted") {
		t.Fatalf("aborted adopt = %v", err)
	}
}

// TestTrackerPrepareReplaceNormalizesDeeplyEqual covers the whole-root no-op.
func TestTrackerPrepareReplaceNormalizesDeeplyEqual(t *testing.T) {
	initial := map[string]any{"nested": map[string]any{"value": 1}, "rows": []any{1, 2, 3}}
	tracker := Track(initial)
	prepared, err := tracker.PrepareReplace(map[string]any{"nested": map[string]any{"value": 1}, "rows": []any{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Ops()) != 0 {
		t.Fatalf("ops = %+v", prepared.Ops())
	}
	mustAdopt(t, tracker, prepared)
	if mapPointer(tracker.Value()) != mapPointer(initial) {
		t.Fatal("deeply equal replacement changed the committed identity")
	}
	if tracker.Revision() != 1 {
		t.Fatalf("revision = %d", tracker.Revision())
	}
}

// TestTrackerPrepareReplaceOwnsReplacement covers the replace batch.
func TestTrackerPrepareReplaceOwnsReplacement(t *testing.T) {
	tracker := Track(map[string]any{"value": 0, "rows": []any{}})
	replacement := map[string]any{"value": 1, "rows": []any{map[string]any{"value": 2}}}
	prepared, err := tracker.PrepareReplace(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if !trackerJSONEqual(t, prepared.Base(), tracker.Value()) {
		t.Fatalf("base = %+v", prepared.Base())
	}
	if mapPointer(prepared.Value()) != mapPointer(replacement) {
		t.Fatal("replacement was copied instead of owned")
	}
	if len(prepared.Ops()) != 1 || prepared.Ops()[0].Verb != VerbReplace {
		t.Fatalf("ops = %+v", prepared.Ops())
	}
	replayed, err := ApplyImmutable(prepared.Base(), prepared.Ops())
	if err != nil {
		t.Fatal(err)
	}
	if !trackerJSONEqual(t, replayed, replacement) {
		t.Fatalf("replayed = %+v", replayed)
	}
	mustAdopt(t, tracker, prepared)
	if mapPointer(tracker.Value()) != mapPointer(replacement) {
		t.Fatal("adoption did not own the replacement")
	}
}

// TestApplyImmutableBatches covers the ordered-backlog helper.
func TestApplyImmutableBatches(t *testing.T) {
	base := map[string]any{"value": float64(0)}
	first, err := ApplyImmutableBatches(base, [][]Op{
		{{Verb: VerbSet, Path: Path{"value"}, Value: float64(1)}},
		{{Verb: VerbSet, Path: Path{"value"}, Value: float64(2)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if value := first.(map[string]any)["value"]; value != float64(2) {
		t.Fatalf("value = %v", value)
	}
	if value := base["value"]; value != float64(0) {
		t.Fatalf("base mutated: %v", value)
	}
}
