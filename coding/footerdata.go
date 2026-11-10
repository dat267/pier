package coding

import (
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Port of core/footer-data-provider.ts: the footer data source (git branch,
// extension statuses, available provider count) used by the interactive mode.
//
// Divergences: upstream uses fs.watch/fs.watchFile (with WSL polling); the Go
// port polls HEAD (and a reftable repository's table list, as upstream watches
// both) on a ticker instead, the same polling approach as the theme watcher
// (D75). The branch resolution is synchronous inside the poll goroutine
// instead of the upstream async exec.

// FooterDataProviderOptions configure the provider.
type FooterDataProviderOptions struct {
	// PollIntervalMS overrides the HEAD poll interval (default 500ms). Zero
	// uses the default; negative disables watching.
	PollIntervalMS int
	// DisableWatch turns off the poll watcher (tests).
	DisableWatch bool
}

// FooterDataProvider provides git branch and extension statuses.
type FooterDataProvider struct {
	// D149: retained cross-goroutine lock. It protects cwd, git paths/branch,
	// statuses and the listener registry shared by footer rendering and the
	// 500 ms git-HEAD watcher. See DECISIONS.md and the ARCHITECTURE.md lock
	// inventory.
	mu sync.Mutex

	cwd      string
	gitPaths *GitPaths
	// reftableTablesList is the reftable table list to poll in addition to
	// HEAD, or "" when the repository is not using the reftable backend
	// (resolved once, like upstream's existsSync check at setup).
	reftableTablesList string
	branch             *string // nil = unresolved; "" = detached (upstream null)
	resolved           bool
	statuses           map[string]string
	providers          int
	callbacks          map[int]func()
	nextCB             int
	disposed           bool

	stopWatch chan struct{}
	watchDone chan struct{}
}

// NewFooterDataProvider creates the provider for a working directory.
func NewFooterDataProvider(cwd string, options FooterDataProviderOptions) *FooterDataProvider {
	gitPaths := FindGitPaths(cwd)
	provider := &FooterDataProvider{
		cwd:                cwd,
		gitPaths:           gitPaths,
		reftableTablesList: reftableTablesListPath(gitPaths),
		statuses:           map[string]string{},
		callbacks:          map[int]func(){},
	}
	if !options.DisableWatch {
		interval := 500 * time.Millisecond
		if options.PollIntervalMS != 0 {
			interval = time.Duration(options.PollIntervalMS) * time.Millisecond
		}
		if interval > 0 {
			provider.stopWatch = make(chan struct{})
			provider.watchDone = make(chan struct{})
			// The baseline is read here, not in the goroutine: a switch made
			// before the scheduler first runs it must still differ from it
			// (A482).
			baseline := provider.readGitState()
			go provider.watchLoop(provider.stopWatch, provider.watchDone, interval, baseline)
		}
	}
	return provider
}

// Cwd returns the provider's working directory.
func (f *FooterDataProvider) Cwd() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cwd
}

// GetGitBranch returns the current branch, "" for detached HEAD, or nil when
// not in a repository (the bool is false for nil).
func (f *FooterDataProvider) GetGitBranch() (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.resolved {
		branch, ok := f.resolveGitBranch()
		if !ok {
			f.branch = nil
		} else {
			f.branch = &branch
		}
		f.resolved = true
	}
	if f.branch == nil {
		return "", false
	}
	return *f.branch, true
}

// GetExtensionStatuses returns a copy of the extension status texts.
func (f *FooterDataProvider) GetExtensionStatuses() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	copyOf := make(map[string]string, len(f.statuses))
	maps.Copy(copyOf, f.statuses)
	return copyOf
}

// OnBranchChange subscribes to git branch changes.
func (f *FooterDataProvider) OnBranchChange(callback func()) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextCB
	f.nextCB++
	f.callbacks[id] = callback
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		delete(f.callbacks, id)
	}
}

// SetExtensionStatus sets or clears an extension status text.
func (f *FooterDataProvider) SetExtensionStatus(key string, text *string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if text == nil {
		delete(f.statuses, key)
		return
	}
	f.statuses[key] = *text
}

// ClearExtensionStatuses clears all extension status texts.
func (f *FooterDataProvider) ClearExtensionStatuses() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses = map[string]string{}
}

// GetAvailableProviderCount returns the provider count shown in the footer.
func (f *FooterDataProvider) GetAvailableProviderCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.providers
}

// SetAvailableProviderCount updates the provider count.
func (f *FooterDataProvider) SetAvailableProviderCount(count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.providers = count
}

// SetCwd changes the working directory and re-resolves the repository.
func (f *FooterDataProvider) SetCwd(cwd string) {
	f.mu.Lock()
	if f.cwd == cwd {
		f.mu.Unlock()
		return
	}
	f.cwd = cwd
	f.resolved = false
	f.branch = nil
	f.gitPaths = FindGitPaths(cwd)
	f.reftableTablesList = reftableTablesListPath(f.gitPaths)
	f.mu.Unlock()
	f.notifyBranchChange()
}

// Dispose stops the watcher and clears the callbacks.
func (f *FooterDataProvider) Dispose() {
	f.mu.Lock()
	if f.disposed {
		f.mu.Unlock()
		return
	}
	f.disposed = true
	stopWatch := f.stopWatch
	watchDone := f.watchDone
	f.stopWatch = nil
	f.watchDone = nil
	f.callbacks = map[int]func(){}
	f.mu.Unlock()
	if stopWatch != nil {
		close(stopWatch)
		<-watchDone
	}
}

func (f *FooterDataProvider) notifyBranchChange() {
	f.mu.Lock()
	ids := make([]int, 0, len(f.callbacks))
	for id := range f.callbacks {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	callbacks := make([]func(), 0, len(ids))
	for _, id := range ids {
		callbacks = append(callbacks, f.callbacks[id])
	}
	f.mu.Unlock()
	for _, callback := range callbacks {
		callback()
	}
}

// Refresh re-resolves the branch and notifies on change. It is exported for
// tests and for hosts that manage their own watcher.
func (f *FooterDataProvider) Refresh() {
	f.mu.Lock()
	if f.disposed {
		f.mu.Unlock()
		return
	}
	next, ok := f.resolveGitBranch()
	changed := f.resolved && ((f.branch == nil && ok) || (f.branch != nil && (!ok || *f.branch != next)))
	if !ok {
		f.branch = nil
	} else {
		f.branch = &next
	}
	f.resolved = true
	f.mu.Unlock()
	if changed {
		f.notifyBranchChange()
	}
}

// watchLoop polls the git state against lastState, the state when the
// provider was created. The channels are passed in because Dispose nils the
// fields (a double dispose must not close a nil channel).
func (f *FooterDataProvider) watchLoop(stopWatch, watchDone chan struct{}, interval time.Duration, lastState string) {
	defer close(watchDone)
	if footerWatchStartHook != nil {
		footerWatchStartHook()
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stopWatch:
			return
		case <-ticker.C:
			current := f.readGitState()
			if current == lastState {
				continue
			}
			lastState = current
			f.Refresh()
		}
	}
}

// footerWatchStartHook runs when a watcher goroutine starts, before it does
// anything else. Nil outside tests, which use it to hold the goroutine back the
// way a busy scheduler can.
var footerWatchStartHook func()

// readGitState returns the git files the footer derives its branch from: HEAD
// and, in a reftable repository, the table list. Upstream watches both
// (`core/footer-data-provider.ts` watches `.git/reftable` and
// `.git/reftable/tables.list`) because "branch switches update files in the
// reftable directory instead of HEAD"; there HEAD is permanently
// `ref: refs/heads/.invalid`, so watching HEAD alone never notices a switch.
func (f *FooterDataProvider) readGitState() string {
	f.mu.Lock()
	gitPaths := f.gitPaths
	tablesListPath := f.reftableTablesList
	f.mu.Unlock()
	if gitPaths == nil {
		return ""
	}
	head, err := os.ReadFile(gitPaths.HeadPath)
	if err != nil {
		return ""
	}
	state := DecodeUTF8WithReplacement(head)
	if tablesListPath == "" {
		return state
	}
	tablesList, err := os.ReadFile(tablesListPath)
	if err != nil {
		return state
	}
	return state + "\x00" + DecodeUTF8WithReplacement(tablesList)
}

// reftableTablesListPath returns the reftable table list of a repository that
// uses the reftable ref backend, or "" (upstream footer-data-provider.ts
// checks for .git/reftable and then its tables.list once, at setup).
func reftableTablesListPath(gitPaths *GitPaths) string {
	if gitPaths == nil {
		return ""
	}
	reftableDir := filepath.Join(gitPaths.CommonGitDir, "reftable")
	if info, err := os.Stat(reftableDir); err != nil || !info.IsDir() {
		return ""
	}
	tablesList := filepath.Join(reftableDir, "tables.list")
	if !fileExists(tablesList) {
		return ""
	}
	return tablesList
}

// resolveGitBranch resolves the branch from HEAD (caller holds the lock).
func (f *FooterDataProvider) resolveGitBranch() (string, bool) {
	if f.gitPaths == nil {
		return "", false
	}
	content, err := os.ReadFile(f.gitPaths.HeadPath)
	if err != nil {
		return "", false
	}
	trimmed := strings.TrimSpace(DecodeUTF8WithReplacement(content))
	if strings.HasPrefix(trimmed, "ref: refs/heads/") {
		branch := trimmed[len("ref: refs/heads/"):]
		if branch == ".invalid" {
			if resolved := ResolveGitBranch(f.gitPaths.RepoDir); resolved != "" {
				return resolved, true
			}
			return "detached", true
		}
		return branch, true
	}
	return "detached", true
}

// ReadonlyFooterDataProvider is the extension-facing read-only view.
type ReadonlyFooterDataProvider interface {
	GetGitBranch() (string, bool)
	GetExtensionStatuses() map[string]string
	GetAvailableProviderCount() int
	OnBranchChange(callback func()) func()
}

var _ ReadonlyFooterDataProvider = (*FooterDataProvider)(nil)
