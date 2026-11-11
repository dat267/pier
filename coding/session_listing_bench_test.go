package coding

import (
	"context"
	"testing"
)

// BenchmarkListSessions pins the cost of /resume's listing: each file is read
// once, ten at a time, and the rows found so far are published as batches
// finish (upstream buildSessionInfosWithConcurrency, MAX_CONCURRENT_SESSION_INFO_LOADS
// = 10, CURRENT_SESSION_LIST_PUBLISH_INTERVAL = 10).
func BenchmarkListSessions(b *testing.B) {
	dir := writeListingFixtureForBench(b, 12, 1<<20)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		sessions, err := ListSessionsWithProgress(context.Background(), dir, dir, nil)
		if err != nil || len(sessions) != 12 {
			b.Fatalf("sessions = %d, err = %v", len(sessions), err)
		}
	}
}

// BenchmarkListSessionsProgress measures the same listing while publishing a
// partial list, which is the path the picker uses.
func BenchmarkListSessionsProgress(b *testing.B) {
	dir := writeListingFixtureForBench(b, 12, 1<<20)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		updates := 0
		sessions, err := ListSessionsWithProgress(context.Background(), dir, dir, func(int, int, []SessionInfo) { updates++ })
		if err != nil || len(sessions) != 12 || updates == 0 {
			b.Fatalf("sessions = %d, updates = %d, err = %v", len(sessions), updates, err)
		}
	}
}
