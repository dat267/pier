package coding

import "testing"

// TestSessionManagerCachesBranch pins that GetBranch("") reuses one walk: the
// footer asks for context usage on every paint, and rebuilding the branch of a
// long session is O(entries) (12.6 ms on a 15.5k-entry session).
func TestSessionManagerCachesBranch(t *testing.T) {
	m, _ := newTestSession(t)
	m.AppendMessage(createUserMessage("one"))
	m.AppendMessage(createUserMessage("two"))

	first := m.GetBranch("")
	second := m.GetBranch("")
	if len(first) == 0 {
		t.Fatal("empty branch")
	}
	if &first[0] != &second[0] {
		t.Fatal("GetBranch rebuilt the branch instead of reusing the cached walk")
	}
	// The shared result must not have spare capacity: a caller appending to it
	// would write into the cache's backing array.
	if cap(first) != len(first) {
		t.Fatalf("cached branch cap = %d, want %d", cap(first), len(first))
	}

	// A new entry invalidates the cache.
	m.AppendMessage(createUserMessage("three"))
	third := m.GetBranch("")
	if len(third) != len(first)+1 {
		t.Fatalf("branch after append = %d entries, want %d", len(third), len(first)+1)
	}
	if &third[0] == &first[0] {
		t.Fatal("append did not invalidate the cached branch")
	}

	// A leaf move invalidates the cache too.
	tree := m.GetEntries()
	if len(tree) >= 2 {
		target := tree[0].ID
		path := m.GetBranch(target)
		if len(path) == 0 {
			t.Fatal("explicit branch is empty")
		}
	}
}

// TestSessionManagerBranchCacheSurvivesFromID checks the cached branch is the
// current leaf's, not an explicitly requested one.
func TestSessionManagerBranchCacheSurvivesFromID(t *testing.T) {
	m, _ := newTestSession(t)
	m.AppendMessage(createUserMessage("one"))
	m.AppendMessage(createUserMessage("two"))
	entries := m.GetEntries()
	firstID := entries[0].ID

	cachedLeaf := m.GetBranch("")
	explicit := m.GetBranch(firstID)
	if len(explicit) >= len(cachedLeaf) {
		t.Fatalf("explicit branch %d entries, leaf branch %d", len(explicit), len(cachedLeaf))
	}
	// The explicit walk must not have replaced the leaf cache.
	again := m.GetBranch("")
	if &again[0] != &cachedLeaf[0] {
		t.Fatal("explicit GetBranch(fromID) clobbered the leaf cache")
	}
}

// Reference: pi v1.3.0 core/session-manager.ts getTree compares Date.getTime,
// not timestamp strings. Equal millisecond values retain insertion order.
func TestSessionTreeOrdersChildrenByJavaScriptTime(t *testing.T) {
	for _, tc := range []struct {
		name, first, second string
		firstWins           bool
	}{
		{"timezone offset", "2026-10-20T01:00:00Z", "2026-10-20T02:00:00+02:00", false},
		{"omitted fractional seconds", "2026-10-20T00:00:00.100Z", "2026-10-20T00:00:00Z", false},
		{"same millisecond", "2026-10-20T00:00:00.0009Z", "2026-10-20T00:00:00.0001Z", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: boolPtr(false)})
			root := manager.AppendMessage(createUserMessage("root"))
			first := manager.AppendMessage(createUserMessage("first"))
			if err := manager.Branch(root); err != nil {
				t.Fatal(err)
			}
			second := manager.AppendMessage(createUserMessage("second"))
			manager.byID[first].Timestamp = tc.first
			manager.byID[second].Timestamp = tc.second
			roots := manager.GetTree()
			if len(roots) != 1 || len(roots[0].Children) != 2 {
				t.Fatalf("invalid fixture tree=%v", roots)
			}
			want := second
			if tc.firstWins {
				want = first
			}
			if got := roots[0].Children[0].Entry.ID; got != want {
				t.Fatalf("first child=%s want=%s", got, want)
			}
		})
	}
}
