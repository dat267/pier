package durable

import (
	"context"
	"sync"
)

// Port of tools/file-mutation-queue.ts: serialize `edit` and `write` mutations
// of one file within this process, keyed by filesystem id and canonical path,
// whichever environment object the call got.

type fileMutationEntry struct {
	mu   sync.Mutex
	refs int
}

var fileMutationQueues = struct {
	mu   sync.Mutex
	keys map[string]*fileMutationEntry
}{keys: map[string]*fileMutationEntry{}}

// mutationKey is the queue key of one path (upstream mutationKey).
func mutationKey(ctx context.Context, env FileSystem, path string) (string, error) {
	absolute, err := env.AbsolutePath(path, ctx)
	if err != nil {
		return "", err
	}
	canonicalPath, err := canonicalMutationPath(ctx, env, absolute)
	if err != nil {
		return "", err
	}
	return env.ID() + "\x00" + canonicalPath, nil
}

// canonicalMutationPath canonicalizes a path; a file that does not exist yet
// uses its canonical parent joined with its name, so a create and a later
// mutation share one key even under a symlinked directory (upstream canonical).
func canonicalMutationPath(ctx context.Context, env FileSystem, absolutePath string) (string, error) {
	canonical, err := env.CanonicalPath(absolutePath, ctx)
	if err == nil {
		return canonical, nil
	}
	var fileError *FileError
	if !asFileError(err, &fileError) {
		return "", err
	}
	if fileError.Code == FileErrorNotSupported {
		return absolutePath, nil
	}
	if fileError.Code != FileErrorNotFound {
		return "", err
	}
	parent, err := env.JoinPath([]string{absolutePath, ".."}, ctx)
	if err != nil {
		return "", err
	}
	if parent == absolutePath || !hasPathPrefix(absolutePath, parent) {
		return absolutePath, nil
	}
	offset := len(parent)
	if offset < len(absolutePath) && (absolutePath[offset] == '/' || absolutePath[offset] == '\\') {
		offset++
	}
	name := absolutePath[offset:]
	canonicalParent, err := canonicalMutationPath(ctx, env, parent)
	if err != nil {
		return "", err
	}
	return env.JoinPath([]string{canonicalParent, name}, ctx)
}

func hasPathPrefix(path string, prefix string) bool {
	return len(path) >= len(prefix) && path[:len(prefix)] == prefix
}

func asFileError(err error, target **FileError) bool {
	if fileError, ok := err.(*FileError); ok {
		*target = fileError
		return true
	}
	return false
}

// WithFileMutationQueue runs fn with the mutation slot of one file. Concurrent
// calls on one file run in the order their keys resolve; other files and other
// filesystems never wait. It is not a lock against `bash` or other processes.
func WithFileMutationQueue[T any](ctx context.Context, env FileSystem, path string, fn func() (T, error)) (T, error) {
	var zero T
	key, err := mutationKey(ctx, env, path)
	if err != nil {
		return zero, err
	}
	fileMutationQueues.mu.Lock()
	entry := fileMutationQueues.keys[key]
	if entry == nil {
		entry = &fileMutationEntry{}
		fileMutationQueues.keys[key] = entry
	}
	entry.refs++
	fileMutationQueues.mu.Unlock()

	entry.mu.Lock()
	defer func() {
		entry.mu.Unlock()
		fileMutationQueues.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(fileMutationQueues.keys, key)
		}
		fileMutationQueues.mu.Unlock()
	}()
	return fn()
}
