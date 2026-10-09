package durable

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultWatchPollInterval   = 2 * time.Second
	defaultWatchDirectoryLimit = 10_000
	watchHashMaxBytes          = 256 * 1024
	watchHashRecent            = 5 * time.Second
)

type resolvedWatchTarget struct {
	path      string
	recursive bool
	hidden    bool
	names     map[string]struct{}
}

type watchedEntry struct {
	kind    FileKind
	info    os.FileInfo
	size    int64
	mtime   int64
	hash    [32]byte
	hasHash bool
}

type osFileWatcher struct {
	mode       WatchMode
	cancel     context.CancelFunc
	done       chan struct{}
	mu         sync.Mutex
	closed     bool
	inCallback bool
}

func (w *osFileWatcher) Mode() WatchMode { return w.mode }

func (w *osFileWatcher) Close(_ context.Context) error {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		w.cancel()
	}
	inCallback := w.inCallback
	w.mu.Unlock()
	if !inCallback {
		<-w.done
	}
	return nil
}

// D215: the standard-library port polls snapshots on every platform instead of selecting native fs watchers.
func (e *OSFileSystem) Watch(targets []WatchTarget, onChange func(WatchChange), ctx context.Context) (FileWatcher, error) {
	if onChange == nil {
		return nil, &FileError{Code: FileErrorInvalid, Message: "watch requires an onChange callback"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, toFileError(err, "")
	}
	resolved := make([]resolvedWatchTarget, 0, len(targets))
	for _, target := range targets {
		path, err := e.resolvePath(target.Path)
		if err != nil {
			return nil, err
		}
		names := map[string]struct{}{}
		var hidden bool
		if target.Exclude != nil {
			hidden = target.Exclude.Hidden
			for _, name := range target.Exclude.Names {
				names[name] = struct{}{}
			}
		}
		resolved = append(resolved, resolvedWatchTarget{path: path, recursive: target.Recursive, hidden: hidden, names: names})
	}
	interval := e.watchPollInterval
	if interval <= 0 {
		interval = defaultWatchPollInterval
	}
	budget := e.maxWatchDirectories
	if budget <= 0 {
		budget = defaultWatchDirectoryLimit
	}
	snapshot, err := scanWatchTargets(ctx, resolved, budget)
	if err != nil {
		return nil, err
	}
	watchContext, cancel := context.WithCancel(ctx)
	watcher := &osFileWatcher{mode: WatchModePolling, cancel: cancel, done: make(chan struct{})}
	go pollWatch(watchContext, watcher, resolved, budget, interval, snapshot, onChange)
	return watcher, nil
}

func pollWatch(
	ctx context.Context,
	watcher *osFileWatcher,
	targets []resolvedWatchTarget,
	budget int,
	interval time.Duration,
	previous map[string]watchedEntry,
	onChange func(WatchChange),
) {
	defer close(watcher.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if ctx.Err() != nil {
			return
		}
		next, err := scanWatchTargets(ctx, targets, budget)
		if err != nil {
			if ctx.Err() == nil {
				deliverWatchChange(ctx, watcher, onChange, WatchChange{Error: watchFileError(err)})
			}
			return
		}
		paths := diffWatchSnapshots(previous, next)
		previous = next
		if len(paths) > 0 {
			deliverWatchChange(ctx, watcher, onChange, WatchChange{Paths: paths})
		}
	}
}

func deliverWatchChange(ctx context.Context, watcher *osFileWatcher, callback func(WatchChange), change WatchChange) {
	watcher.mu.Lock()
	if watcher.closed || ctx.Err() != nil {
		watcher.mu.Unlock()
		return
	}
	watcher.inCallback = true
	watcher.mu.Unlock()
	defer func() {
		_ = recover()
		watcher.mu.Lock()
		watcher.inCallback = false
		watcher.mu.Unlock()
	}()
	callback(change)
}

func scanWatchTargets(ctx context.Context, targets []resolvedWatchTarget, directoryLimit int) (map[string]watchedEntry, error) {
	snapshot := map[string]watchedEntry{}
	directories := map[string]struct{}{}
	countDirectory := func(path string) error {
		if _, present := directories[path]; present {
			return nil
		}
		directories[path] = struct{}{}
		if len(directories) > directoryLimit {
			return &FileError{Code: FileErrorInvalid, Message: "Watched paths exceed directory limit", Path: path}
		}
		return nil
	}
	var visit func(target resolvedWatchTarget, directory string) error
	visit = func(target resolvedWatchTarget, directory string) error {
		dir, err := os.Open(directory)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.ENOTDIR) {
				return nil
			}
			return toFileError(err, directory)
		}
		defer dir.Close()
		var subdirectories []string
		for {
			entries, readErr := dir.ReadDir(256)
			for _, entry := range entries {
				if ctx.Err() != nil {
					return toFileError(ctx.Err(), directory)
				}
				name := entry.Name()
				if target.hidden && strings.HasPrefix(name, ".") {
					continue
				}
				if _, excluded := target.names[name]; excluded {
					continue
				}
				path := filepath.Join(directory, name)
				info, err := os.Lstat(path)
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				if err != nil {
					return toFileError(err, path)
				}
				if _, err := snapshotEntry(snapshot, path, info); err != nil {
					return err
				}
				if target.recursive && info.IsDir() {
					subdirectories = append(subdirectories, path)
				}
			}
			if readErr == nil {
				continue
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if errors.Is(readErr, fs.ErrNotExist) || errors.Is(readErr, fs.ErrPermission) || errors.Is(readErr, syscall.ENOTDIR) {
				return nil
			}
			return toFileError(readErr, directory)
		}
		if err := dir.Close(); err != nil {
			return toFileError(err, directory)
		}
		for _, path := range subdirectories {
			if err := countDirectory(path); err != nil {
				return err
			}
			if err := visit(target, path); err != nil {
				return err
			}
		}
		return nil
	}
	for _, target := range targets {
		if ctx.Err() != nil {
			return nil, toFileError(ctx.Err(), target.path)
		}
		info, err := os.Stat(target.path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, toFileError(err, target.path)
		}
		if _, err := snapshotEntry(snapshot, target.path, info); err != nil {
			return nil, err
		}
		if info.IsDir() {
			if err := countDirectory(target.path); err != nil {
				return nil, err
			}
			if err := visit(target, target.path); err != nil {
				return nil, err
			}
		}
	}
	return snapshot, nil
}

func snapshotEntry(snapshot map[string]watchedEntry, path string, info os.FileInfo) (watchedEntry, error) {
	converted, err := fileInfoFromStat(path, info)
	if err != nil {
		var fileError *FileError
		if errors.As(err, &fileError) && fileError.Code == FileErrorInvalid {
			return watchedEntry{}, nil
		}
		return watchedEntry{}, err
	}
	entry := watchedEntry{kind: converted.Kind, info: info, size: info.Size(), mtime: info.ModTime().UnixNano()}
	if info.Mode().IsRegular() && info.Size() >= 0 && info.Size() <= watchHashMaxBytes && time.Since(info.ModTime()) < watchHashRecent {
		if data, err := os.ReadFile(path); err == nil {
			entry.hash, entry.hasHash = sha256.Sum256(data), true
		}
	}
	snapshot[path] = entry
	return entry, nil
}

func diffWatchSnapshots(previous, next map[string]watchedEntry) []string {
	changed := make([]string, 0)
	for path, current := range next {
		before, present := previous[path]
		if !present || !sameWatchedEntry(before, current) {
			changed = append(changed, path)
		}
	}
	for path := range previous {
		if _, present := next[path]; !present {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}

func sameWatchedEntry(left, right watchedEntry) bool {
	if left.kind != right.kind || !os.SameFile(left.info, right.info) {
		return false
	}
	if left.kind == FileKindDirectory {
		return true
	}
	if left.size != right.size || left.mtime != right.mtime || left.hasHash != right.hasHash {
		return false
	}
	return !left.hasHash || left.hash == right.hash
}

func watchFileError(err error) *FileError {
	var fileError *FileError
	if errors.As(err, &fileError) {
		return fileError
	}
	return &FileError{Code: FileErrorUnknown, Message: err.Error(), Cause: err}
}
