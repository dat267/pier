package coding

import "testing"

// tailWindow keeps the newest compaction, the entries from its first kept entry
// up to it (skipping system messages, which the window does not replay), and
// everything after. Non-message entries before the compaction stay, and a
// compaction whose first kept entry is present keeps its target.
func TestTailWindowWindowAndFirstKept(t *testing.T) {
	before := SessionEntry{Type: "message", SessionEntryBase: SessionEntryBase{ID: "before"}}
	custom := SessionEntry{Type: "custom_message", SessionEntryBase: SessionEntryBase{ID: "custom"}}
	compaction := SessionEntry{Type: "compaction", SessionEntryBase: SessionEntryBase{ID: "compaction"}, Summary: "s", FirstKeptEntryID: "custom"}
	after := SessionEntry{Type: "message", SessionEntryBase: SessionEntryBase{ID: "after"}}

	window := tailWindow([]SessionEntry{before, custom, compaction, after})
	gotIDs := make([]string, 0, len(window))
	for _, entry := range window {
		gotIDs = append(gotIDs, entry.ID)
	}
	if len(gotIDs) != 3 || gotIDs[0] != "custom" || gotIDs[1] != "compaction" || gotIDs[2] != "after" {
		t.Fatalf("window = %v, want [custom compaction after]", gotIDs)
	}
	if window[1].FirstKeptEntryID != "custom" {
		t.Fatalf("first kept = %q, want custom preserved", window[1].FirstKeptEntryID)
	}
}

// A first kept entry that is not on the branch rewrites the compaction target to
// the compaction itself, so a resumed window replays the compaction alone.
func TestTailWindowRewritesAMissingFirstKept(t *testing.T) {
	before := SessionEntry{Type: "message", SessionEntryBase: SessionEntryBase{ID: "before"}}
	compaction := SessionEntry{Type: "compaction", SessionEntryBase: SessionEntryBase{ID: "compaction"}, Summary: "s", FirstKeptEntryID: "gone"}
	window := tailWindow([]SessionEntry{before, compaction})
	if len(window) != 1 || window[0].ID != "compaction" {
		t.Fatalf("window = %+v, want the compaction alone", window)
	}
	if window[0].FirstKeptEntryID != "compaction" {
		t.Fatalf("first kept = %q, want the compaction's own id", window[0].FirstKeptEntryID)
	}
}
