package coding

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The footer's git watcher runs beside footer rendering, cwd changes and
// listener churn. Under all of it at once the branch must always be one that
// HEAD named, a listener that reads the provider back must not deadlock, and
// Dispose (called concurrently, more than once) must stop the watcher and the
// notifications. Upstream core/footer-data-provider.ts (pi v1.5.0 f9a26365)
// runs this on one event loop; the port's watcher is a goroutine.
func TestFooterDataProviderSurvivesConcurrentUseAndDispose(t *testing.T) {
	repos := []string{fakeGitRepo(t, "a"), fakeGitRepo(t, "b")}
	provider := NewFooterDataProvider(repos[0], FooterDataProviderOptions{PollIntervalMS: 1})
	var notified atomic.Int64
	provider.OnBranchChange(func() {
		notified.Add(1)
		provider.GetGitBranch() // re-enters the provider
	})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	spin := func(work func(i int)) {
		wg.Go(func() {
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				work(i)
			}
		})
	}
	spin(func(i int) {
		writeHeadAtomically(t, repos[i%2], []string{"main", "feature"}[i%2])
		time.Sleep(200 * time.Microsecond)
	})
	spin(func(int) {
		if branch, ok := provider.GetGitBranch(); ok && branch != "main" && branch != "feature" {
			t.Errorf("branch = %q, not one HEAD named", branch)
		}
	})
	spin(func(int) { provider.Refresh() })
	spin(func(i int) {
		provider.SetCwd(repos[i%2])
		time.Sleep(100 * time.Microsecond)
	})
	spin(func(int) { provider.OnBranchChange(func() {})() })

	// Run for a while and until the stress has produced branch changes; on one
	// CPU under -race the busy spinners can starve the cwd switcher for a time.
	minimum := time.Now().Add(150 * time.Millisecond)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(minimum) || (notified.Load() < 3 && time.Now().Before(deadline)) {
		time.Sleep(5 * time.Millisecond)
	}
	var disposers sync.WaitGroup
	for range 3 {
		disposers.Go(provider.Dispose)
	}
	disposed := make(chan struct{})
	go func() { disposers.Wait(); close(disposed) }()
	select {
	case <-disposed:
	case <-time.After(5 * time.Second):
		t.Fatal("Dispose did not return")
	}
	close(stop)
	wg.Wait()
	if notified.Load() == 0 {
		t.Fatal("the stress never produced a branch change; it tested nothing")
	}

	// Refresh after Dispose is a no-op and notifies no one.
	before := notified.Load()
	writeHeadAtomically(t, repos[0], "after-dispose")
	provider.Refresh()
	time.Sleep(20 * time.Millisecond)
	if got := notified.Load(); got != before {
		t.Fatalf("notified %d times after Dispose", got-before)
	}
}

// fakeGitRepo makes a directory with just enough .git for FindGitPaths.
func fakeGitRepo(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".git", "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeHeadAtomically(t, dir, "main")
	return dir
}

// writeHeadAtomically replaces HEAD by rename, as git does, so a reader never
// sees a partial file.
func writeHeadAtomically(t *testing.T, repo, branch string) {
	head := filepath.Join(repo, ".git", "HEAD")
	temp := head + ".lock"
	if err := os.WriteFile(temp, []byte("ref: refs/heads/"+branch+"\n"), 0o644); err != nil {
		t.Error(err)
		return
	}
	if err := os.Rename(temp, head); err != nil {
		t.Error(err)
	}
}
