package interactive

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// Reference: pi v1.4.0 (4a080252), session-selector-search.ts
// filterAndSortSessions: score first, modified descending, stable on equal keys.
func TestSessionSearchRelevanceSortPreservesTiesAndInput(t *testing.T) {
	rows := []coding.SessionInfo{
		{ID: "aa", AllMessagesText: "prefix needle", Modified: time.UnixMilli(5000)},
		{ID: "bb", AllMessagesText: "needle", Modified: time.UnixMilli(1000)},
		{ID: "cc", AllMessagesText: "needle", Modified: time.UnixMilli(3000)},
		{ID: "dd", AllMessagesText: "needle", Modified: time.UnixMilli(3000)},
		{ID: "ee", AllMessagesText: "prefix needle", Modified: time.UnixMilli(4000)},
	}
	ids := func(rows []coding.SessionInfo) []string {
		result := make([]string, len(rows))
		for i, row := range rows {
			result[i] = row.ID
		}
		return result
	}
	before := ids(rows)
	for _, mode := range []SortMode{SortRelevance, SortThreaded} {
		got := ids(FilterAndSortSessions(rows, `"needle"`, mode, NameFilterAll))
		// pi v1.5.0 8c03fef4d: matches score by tier with no position penalty, so
		// every session here ties on the text tier and recency decides.
		if want := []string{"aa", "ee", "cc", "dd", "bb"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s order = %q, want %q", mode, got, want)
		}
	}
	if got := ids(FilterAndSortSessions(rows, `"needle"`, SortRecent, NameFilterAll)); !reflect.DeepEqual(got, before) {
		t.Fatalf("recent mode reordered rows: %q", got)
	}
	if got := ids(rows); !reflect.DeepEqual(got, before) {
		t.Fatalf("search mutated the source rows: %q", got)
	}
}

// CR-054: one real search keystroke on recency-ordered rows. Relevance moves
// whole groups; the control has identical-size data already relevance-ordered.
// Keep timing out of ordinary tests. Compare ordered/reordered benchmark results:
// go test ./coding/interactive -run '^$' -bench '^BenchmarkSessionSearchRelevance/8000/' -benchtime=1s -count=3
func BenchmarkSessionSearchRelevance(b *testing.B) {
	for _, count := range []int{1000, 4000, 8000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			for _, reordered := range []bool{false, true} {
				label := "ordered"
				if reordered {
					label = "reordered"
				}
				b.Run(label, func(b *testing.B) {
					rows := make([]coding.SessionInfo, count)
					for i := range rows {
						highScore := i >= count/2
						if reordered {
							highScore = !highScore
						}
						text := "needle"
						if highScore {
							text = "prefix needle"
						}
						rows[i] = coding.SessionInfo{
							ID: fmt.Sprintf("s%05d", i), Path: fmt.Sprintf("/synthetic/session-%05d", i),
							Modified: time.Unix(int64(count-i), 0), AllMessagesText: text,
						}
					}
					list := NewSessionList(rows, false, SortRelevance, NameFilterAll, tui.GetKeybindings(), "")
					b.ReportAllocs()
					for b.Loop() {
						list.SearchInput().SetValue(`"needle`)
						list.HandleInput(`"`)
					}
					// v1.5.0 search tiers both groups are equal-tier text
					// matches (no position penalty), so relevance falls back to
					// recency and the newest row wins in either input order.
					if got := list.GetSelectedSessionPath(); got != rows[0].Path {
						b.Fatalf("selected path = %q, want %q", got, rows[0].Path)
					}
				})
			}
		})
	}
}

// The search cache is what keeps a keystroke off the lowercasing of every
// session's message text: an unchanged session reuses its fields, and a changed
// source invalidates only that session.
func TestSessionSearchCacheReusesAndInvalidates(t *testing.T) {
	cache := newSessionSearchCache()
	session := coding.SessionInfo{ID: "s", Path: "/s.jsonl", Name: "name", Cwd: "/work", AllMessagesText: "text"}
	first := cache.fields(session)
	if again := cache.fields(session); again != first {
		t.Fatal("unchanged session recomputed its fields")
	}
	renamed := session
	renamed.Name = "renamed"
	if cache.fields(renamed) == first {
		t.Fatal("a renamed session reused stale fields")
	}
	appended := session
	appended.AllMessagesText += " more"
	if cache.fields(appended) == first {
		t.Fatal("a grown transcript reused stale fields")
	}
	// A list without the session drops its entry.
	cache.retain(map[string]bool{})
	if len(cache.entries) != 0 {
		t.Fatalf("cache kept %d entries after retain", len(cache.entries))
	}
}
