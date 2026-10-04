// Package delta tracker: the revision lifecycle of upstream's delta tracker.
//
// D11 (extended): upstream's tracker hands out a JavaScript Proxy draft whose
// property writes record operations. Go has no proxies, so a Change exposes a
// plain mutable JSON draft and computes its batch with the same diff engine
// ([Diff]) when it settles. The lifecycle, staleness rules, no-op handling and
// the resulting values match upstream; the op shape is not contractual
// upstream ("batches are exact but not canonical"), and the tracker's own
// tuples (append `a`, truncate `t`, permutation `m`) fold into the applier's
// set/splice/replace forms.
package delta

import (
	"errors"
	"fmt"
	"sync"

	"github.com/dat267/pier/chord"
)

// Tracker owns one revision sequence: it starts from a root value, opens
// changes over it, and adopts their preparations (upstream Tracker).
type Tracker struct {
	mu       sync.Mutex
	value    chord.JsonValue
	revision int64
	// generation identifies the current revision: a preparation from an older
	// generation is stale and cannot be adopted.
	generation int64
}

// Track takes ownership of a root value (upstream track). The caller must not
// mutate the value afterwards.
func Track(value chord.JsonValue) *Tracker {
	return &Tracker{value: value}
}

// Value is the latest adopted revision. It is immutable by contract.
func (t *Tracker) Value() chord.JsonValue {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.value
}

// Revision counts adopted preparations; a no-op adoption still advances it
// (upstream Tracker.revision).
func (t *Tracker) Revision() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.revision
}

// BeginChange opens an overlay draft over the current revision. The draft is a
// detached mutable copy; the revision itself is never modified.
func (t *Tracker) BeginChange() *Change {
	t.mu.Lock()
	defer t.mu.Unlock()
	return &Change{
		tracker:    t,
		generation: t.generation,
		base:       t.value,
		state:      chord.CloneJSON(t.value),
	}
}

// PrepareReplace is a whole-root operation, not a diff: a deeply equal value
// keeps the current root and produces no ops, anything else replaces the root
// (upstream prepareReplace).
func (t *Tracker) PrepareReplace(value chord.JsonValue) (*Prepared, error) {
	t.mu.Lock()
	base := t.value
	generation := t.generation
	t.mu.Unlock()
	ops, err := Diff(base, value, nil)
	if err != nil {
		return nil, err
	}
	prepared := &Prepared{tracker: t, generation: generation, base: base}
	if len(ops) == 0 {
		prepared.value = base
		return prepared, nil
	}
	prepared.value = value
	prepared.ops = []Op{{Verb: VerbReplace, Value: chord.CloneJSON(value)}}
	return prepared, nil
}

// Adopt swaps the root pointer to the prepared value and stales every
// competitor (upstream adopt).
func (t *Tracker) Adopt(prepared *Prepared) error {
	if prepared == nil {
		return errors.New("cannot adopt a nil preparation")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if prepared.tracker != t {
		return errors.New("cannot adopt a preparation from another tracker")
	}
	if prepared.aborted {
		return errors.New("cannot adopt an aborted preparation")
	}
	if prepared.adopted {
		return errors.New("preparation is already adopted")
	}
	if prepared.generation != t.generation {
		return errors.New("preparation is stale")
	}
	prepared.adopted = true
	t.value = prepared.value
	t.revision++
	t.generation++
	return nil
}

// Change is one open overlay draft (upstream Change). Its State is mutable only
// while the change is open.
type Change struct {
	tracker    *Tracker
	generation int64
	base       chord.JsonValue
	state      chord.JsonValue
	settled    bool
	prepared   *Prepared
}

// State is the mutable draft. It must not be used after Prepare or Abort.
func (c *Change) State() chord.JsonValue {
	if c.settled {
		panic("delta: the draft is settled")
	}
	return c.state
}

// Prepare materializes the candidate value and its ops without changing
// authority (upstream Change.prepare).
func (c *Change) Prepare() (*Prepared, error) {
	if c.settled {
		return nil, errors.New("change is already settled")
	}
	c.settled = true
	ops, err := Diff(c.base, c.state, nil)
	if err != nil {
		return nil, err
	}
	c.prepared = &Prepared{tracker: c.tracker, generation: c.generation, base: c.base, value: c.state, ops: ops}
	return c.prepared, nil
}

// Abort settles the change. Aborting a settled change also aborts its prepared
// result, so the candidate cannot be adopted (upstream Change.abort).
func (c *Change) Abort() {
	c.settled = true
	if c.prepared != nil {
		c.prepared.Abort()
	}
}

// Settled reports whether the change was prepared or aborted.
func (c *Change) Settled() bool { return c.settled }

// Prepared is a materialized revision that has not been adopted (upstream
// Prepared).
type Prepared struct {
	tracker    *Tracker
	generation int64
	base       chord.JsonValue
	value      chord.JsonValue
	ops        []Op
	aborted    bool
	adopted    bool
}

// Base is the revision the batch applies to. It is immutable by contract.
func (p *Prepared) Base() chord.JsonValue { return p.base }

// Value is the candidate revision. It is immutable by contract.
func (p *Prepared) Value() chord.JsonValue { return p.value }

// Ops is the exact batch transforming Base into Value.
func (p *Prepared) Ops() []Op { return p.ops }

// Abort prevents adoption; the candidate stays readable (upstream
// Prepared.abort).
func (p *Prepared) Abort() { p.aborted = true }

// Aborted reports whether adoption was prevented.
func (p *Prepared) Aborted() bool { return p.aborted }

// Adopted reports whether the preparation was adopted.
func (p *Prepared) Adopted() bool { return p.adopted }

// ApplyImmutableBatches applies an ordered backlog without exposing an
// intermediate revision: one copy-on-write scope spans the whole call (upstream
// applyImmutableBatches).
func ApplyImmutableBatches(target chord.JsonValue, batches [][]Op) (chord.JsonValue, error) {
	root := target
	for _, batch := range batches {
		var err error
		root, err = ApplyImmutable(root, batch)
		if err != nil {
			return nil, err
		}
	}
	return root, nil
}

// String renders the tracker's revision state for diagnostics.
func (t *Tracker) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return fmt.Sprintf("Tracker{revision:%d, generation:%d}", t.revision, t.generation)
}
