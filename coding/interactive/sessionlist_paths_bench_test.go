package interactive

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
)

// sessionListBenchSessions builds n sessions backed by real files, so a
// canonicalizing path lookup pays the lstat/readlink syscalls a real session
// directory does.
func sessionListBenchSessions(b *testing.B, n int) []coding.SessionInfo {
	b.Helper()
	dir := b.TempDir()
	sessions := make([]coding.SessionInfo, 0, n)
	for i := range n {
		path := filepath.Join(dir, fmt.Sprintf("session-%03d.jsonl", i))
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			b.Fatal(err)
		}
		sessions = append(sessions, coding.SessionInfo{
			Path:         path,
			FirstMessage: fmt.Sprintf("session %d", i),
			Modified:     time.Now().Add(-time.Duration(i) * time.Minute),
			MessageCount: i,
		})
	}
	return sessions
}

func BenchmarkSessionListRender(b *testing.B) {
	SetCustomThemesDir(b.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)
	sessions := sessionListBenchSessions(b, 100)
	list := NewSessionList(sessions, false, SortRecent, NameFilterAll, nil, sessions[len(sessions)/2].Path)
	b.ReportAllocs()

	for b.Loop() {
		_ = list.Render(80)
	}
}

func BenchmarkSessionListFilterSessions(b *testing.B) {
	SetCustomThemesDir(b.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)
	sessions := sessionListBenchSessions(b, 1000)
	list := NewSessionList(sessions, false, SortThreaded, NameFilterAll, nil, "")
	b.ReportAllocs()

	for b.Loop() {
		list.FilterSessions("")
	}
}
