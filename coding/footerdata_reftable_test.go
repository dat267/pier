package coding

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// pi v1.5.0 (f9a26365) core/footer-data-provider.ts watches .git/reftable and
// .git/reftable/tables.list in addition to HEAD, because "branch switches
// update files in the reftable directory instead of HEAD". In such a repository
// HEAD is permanently `ref: refs/heads/.invalid`, so a watcher that only
// compares HEAD's content never notices a switch and the footer keeps the
// branch it resolved at startup.
func TestFooterDataProviderSeesBranchSwitchInReftableRepo(t *testing.T) {
	dir, run := newGitRepo(t, "reftable")
	if _, err := os.Stat(filepath.Join(dir, ".git", "reftable", "tables.list")); err != nil {
		t.Skipf("no reftable tables.list: %v", err)
	}
	provider := NewFooterDataProvider(dir, FooterDataProviderOptions{PollIntervalMS: 20})
	defer provider.Dispose()
	if branch, ok := provider.GetGitBranch(); !ok || branch == "" {
		t.Fatalf("initial branch = %q, %v; want a name", branch, ok)
	}

	// The watcher surfaces the switch on its next poll.
	run("switch", "-q", "-c", "feature")
	waitForBranch(t, provider, "feature")
}

// A switch made before the watcher goroutine first runs is still seen: the
// baseline the poll compares against is the state at construction, like
// upstream's fs.watch, which is armed synchronously in the constructor
// (core/footer-data-provider.ts). Taking it when the goroutine started missed
// the switch whenever the scheduler ran the goroutine late (A482: one CPU or a
// loaded machine), and the footer kept the startup branch.
func TestFooterDataProviderSeesASwitchMadeBeforeTheWatcherRuns(t *testing.T) {
	for _, format := range []string{"files", "reftable"} {
		t.Run(format, func(t *testing.T) {
			dir, run := newGitRepo(t, format)
			release := make(chan struct{})
			started := make(chan struct{})
			footerWatchStartHook = func() { close(started); <-release }
			t.Cleanup(func() { footerWatchStartHook = nil })
			provider := NewFooterDataProvider(dir, FooterDataProviderOptions{PollIntervalMS: 20})
			defer provider.Dispose()
			if branch, ok := provider.GetGitBranch(); !ok || branch == "" {
				t.Fatalf("initial branch = %q, %v; want a name", branch, ok)
			}
			<-started
			run("switch", "-q", "-c", "feature")
			close(release)
			waitForBranch(t, provider, "feature")
		})
	}
}

// TestFooterDataProviderSeesBranchSwitchInFilesRepo is the control for the
// reftable case: with the files ref backend HEAD itself names the branch, and
// the same watcher follows a switch.
func TestFooterDataProviderSeesBranchSwitchInFilesRepo(t *testing.T) {
	dir, run := newGitRepo(t, "files")
	provider := NewFooterDataProvider(dir, FooterDataProviderOptions{PollIntervalMS: 20})
	defer provider.Dispose()

	run("switch", "-q", "-c", "feature")
	waitForBranch(t, provider, "feature")
}

// newGitRepo creates a repository with the given ref backend and one commit.
func newGitRepo(t *testing.T, refFormat string) (string, func(args ...string)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		command := exec.Command("git", args...)
		command.Dir = dir
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if output, err := exec.Command("git", "init", "-q", "--ref-format="+refFormat, dir).CombinedOutput(); err != nil {
		t.Skipf("git cannot create a %s repository: %v: %s", refFormat, err, output)
	}
	run("commit", "-q", "--allow-empty", "-m", "one")
	return dir, run
}

// waitForBranch polls the provider until it reports want.
func waitForBranch(t *testing.T, provider *FooterDataProvider, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if branch, _ := provider.GetGitBranch(); branch == want {
			return
		}
		if time.Now().After(deadline) {
			branch, _ := provider.GetGitBranch()
			t.Fatalf("branch = %q, want %q", branch, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Reference: pi v1.5.2 core/footer-data-provider.ts dispose() only clears the
// timers and listeners, so a branch-change callback may dispose the provider.
// The port waits for its poll goroutine, which is the goroutine delivering the
// callback, so that call deadlocked the process.
func TestFooterDataProviderDisposeFromBranchCallbackDoesNotDeadlock(t *testing.T) {
	dir, run := newGitRepo(t, "files")
	provider := NewFooterDataProvider(dir, FooterDataProviderOptions{PollIntervalMS: 20})
	// Resolving first makes the switch a change rather than the initial read.
	if branch, ok := provider.GetGitBranch(); !ok || branch == "" {
		t.Fatalf("initial branch = %q, %v; want a name", branch, ok)
	}
	disposed := make(chan struct{})
	provider.OnBranchChange(func() {
		select {
		case <-disposed:
			return
		default:
		}
		close(disposed)
		provider.Dispose()
	})

	run("switch", "-q", "-c", "feature")
	select {
	case <-disposed:
	case <-time.After(5 * time.Second):
		t.Fatal("the branch callback never returned")
	}
	// The watcher must still be stopped and joined.
	select {
	case <-time.After(100 * time.Millisecond):
	}
	provider.Dispose()
}

// pi v1.4.0 core/footer-data-provider.ts stores listeners in a Set, whose
// iteration order follows registration order.
func TestFooterDataProviderNotifiesListenersInRegistrationOrder(t *testing.T) {
	root := t.TempDir()
	provider := NewFooterDataProvider(root, FooterDataProviderOptions{DisableWatch: true})
	defer provider.Dispose()

	const listenerCount = 24
	var got []int
	for i := 0; i < listenerCount; i++ {
		index := i
		provider.OnBranchChange(func() { got = append(got, index) })
	}

	provider.SetCwd(filepath.Join(root, "next"))
	if len(got) != listenerCount {
		t.Fatalf("got %d notifications, want %d", len(got), listenerCount)
	}
	for i, index := range got {
		if index != i {
			t.Fatalf("notification order = %v, want registration order 0..%d", got, listenerCount-1)
		}
	}
}
