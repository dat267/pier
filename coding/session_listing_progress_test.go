package coding

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeListingFixture writes count session files of about size bytes each, so a
// listing has real work to do per file.
func writeListingFixture(t *testing.T, count int, size int) string {
	t.Helper()
	dir := t.TempDir()
	body := make([]byte, size)
	for index := range body {
		body[index] = 'x'
	}
	text := string(body)
	for index := range count {
		name := fmt.Sprintf("%02d.jsonl", index)
		content := `{"type":"session","version":3,"id":"s` + name + `","timestamp":"2026-01-01T00:00:00.000Z","cwd":"` + dir + `"}` + "\n" +
			`{"type":"message","id":"m` + name + `","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"user","content":[{"type":"text","text":"` + text + `"}]}}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Upstream listSessionsFromDir publishes a sorted partial list as files finish
// (first file, every CURRENT_SESSION_LIST_PUBLISH_INTERVAL files, and the last),
// so the picker shows rows while the rest still load.
func TestListSessionsWithProgressReportsPartials(t *testing.T) {
	dir := writeListingFixture(t, 12, 1024)
	var updates []struct {
		loaded int
		total  int
		rows   int
	}
	sessions, err := ListSessionsWithProgress(context.Background(), dir, dir, func(loaded int, total int, partial []SessionInfo) {
		updates = append(updates, struct {
			loaded int
			total  int
			rows   int
		}{loaded: loaded, total: total, rows: len(partial)})
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 12 {
		t.Fatalf("sessions = %d, want 12", len(sessions))
	}
	if len(updates) < 3 {
		t.Fatalf("progress updates = %d, want at least 3 (first, every 10, last): %v", len(updates), updates)
	}
	if updates[0].loaded != 1 || updates[0].rows != 1 {
		t.Fatalf("first update = %+v, want one row of one file", updates[0])
	}
	for index, update := range updates {
		if update.total != 12 {
			t.Fatalf("update %d total = %d, want 12", index, update.total)
		}
		if index > 0 && update.loaded <= updates[index-1].loaded {
			t.Fatalf("loaded did not grow: %v", updates)
		}
		if update.rows != update.loaded {
			t.Fatalf("update %d carried %d rows for %d files", index, update.rows, update.loaded)
		}
	}
	if last := updates[len(updates)-1]; last.loaded != 12 || last.rows != 12 {
		t.Fatalf("last update = %+v, want all 12", last)
	}
}

// A cancelled load stops instead of reading every file: the picker cancels when
// the user switches scope, and the port used to keep scanning.
func TestListSessionsWithProgressStopsOnCancel(t *testing.T) {
	dir := writeListingFixture(t, 12, 4096)
	ctx, cancel := context.WithCancel(context.Background())
	seen := 0
	_, err := ListSessionsWithProgress(ctx, dir, dir, func(loaded int, total int, partial []SessionInfo) {
		seen = loaded
		if seen >= 1 {
			cancel()
		}
	})
	if err == nil {
		t.Fatalf("a cancelled listing returned no error (read %d of 12 files)", seen)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if seen >= 12 {
		t.Fatalf("the listing read every file after cancellation: %d", seen)
	}
}

// writeListingFixtureForBench is writeListingFixture for benchmarks.
func writeListingFixtureForBench(b *testing.B, count int, size int) string {
	b.Helper()
	dir := b.TempDir()
	body := make([]byte, size)
	for index := range body {
		body[index] = 'x'
	}
	text := string(body)
	for index := range count {
		name := fmt.Sprintf("%02d.jsonl", index)
		content := `{"type":"session","version":3,"id":"s` + name + `","timestamp":"2026-01-01T00:00:00.000Z","cwd":"` + dir + `"}` + "\n" +
			`{"type":"message","id":"m` + name + `","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"user","content":[{"type":"text","text":"` + text + `"}]}}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	return dir
}
