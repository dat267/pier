package interactive

import (
	"fmt"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
)

// sessionTreeFixture builds count sessions whose modified times rise with the
// index, so the newest session has the highest index.
func sessionTreeFixture(count int) []coding.SessionInfo {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sessions := make([]coding.SessionInfo, 0, count)
	for index := 0; index < count; index++ {
		sessions = append(sessions, coding.SessionInfo{
			Path:     fmt.Sprintf("/agent/sessions/s-%05d.jsonl", index),
			Modified: base.Add(time.Duration(index) * time.Minute),
		})
	}
	return sessions
}

// TestSessionTreeSortsByLatestActivityDescending pins pi v1.5.2 (b4afdddf)
// modes/interactive/components/session-selector.ts buildSessionTree: roots and
// children sort by `b.latestActivity - a.latestActivity`, and a parent's
// activity includes its newest child. Ties keep their input order, as the
// reference's stable Array.prototype.sort does.
func TestSessionTreeSortsByLatestActivityDescending(t *testing.T) {
	sessions := sessionTreeFixture(5)
	// A child older than its parent, and two roots with the same activity.
	sessions[4].ParentSessionPath = sessions[1].Path
	sessions[2].Modified = sessions[0].Modified
	roots := buildSessionTree(sessions, sessionPathKey)
	var order []string
	for _, root := range roots {
		order = append(order, root.Session.Path)
		if len(root.Children) > 0 {
			for index, child := range root.Children {
				if index > 0 && root.Children[index-1].LatestActivity < child.LatestActivity {
					t.Fatalf("children are not newest first: %+v", root.Children)
				}
			}
		}
	}
	// Session 1 holds session 4 as a child, so its activity is the newest; then
	// 3, then the two roots with equal activity in input order.
	want := []string{sessions[1].Path, sessions[3].Path, sessions[0].Path, sessions[2].Path}
	if len(order) != len(want) {
		t.Fatalf("roots = %v, want %v", order, want)
	}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("roots = %v, want %v", order, want)
		}
	}
	if parent := roots[0]; len(parent.Children) != 1 || parent.Children[0].Session.Path != sessions[4].Path {
		t.Fatalf("parent children = %+v", parent.Children)
	}
	if parent := roots[0]; parent.LatestActivity != sessions[4].Modified.UnixMilli() {
		t.Fatalf("parent activity = %d, want the child's %d", parent.LatestActivity, sessions[4].Modified.UnixMilli())
	}
}

// BenchmarkBuildSessionTreeAllSessions covers the tree rebuild that every loader
// progress update and filter change runs. With the reference's stable comparison
// sort the rebuild takes 0.86/3.4/18.2 ms at 500/2,000/10,000 sessions; the
// port's former insertion sort took 2.2/10.2/125.3 ms, and the pinned
// session-selector.ts (with its own path keys) takes 16/40/190 ms on the same
// fixture.
func BenchmarkBuildSessionTreeAllSessions(b *testing.B) {
	for _, count := range []int{500, 2000, 10000} {
		sessions := sessionTreeFixture(count)
		b.Run(fmt.Sprintf("sessions-%d", count), func(b *testing.B) {
			for b.Loop() {
				if roots := buildSessionTree(sessions, sessionPathKey); len(roots) != count {
					b.Fatalf("roots = %d, want %d", len(roots), count)
				}
			}
		})
	}
}
