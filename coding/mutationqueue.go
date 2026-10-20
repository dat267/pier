package coding

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// Port of core/tools/file-mutation-queue.ts: serialize file mutations
// targeting the same file; operations for different files run in parallel.
// Upstream keys queues by realpath (falling back to the resolved path for
// missing files).

var (
	mutationQueues   map[string]*fileMutationQueue
	mutationQueuesMu sync.Mutex
)

func init() {
	mutationQueues = map[string]*fileMutationQueue{}
}

// getMutationQueueKey resolves the queue key by realpath, falling back to
// the resolved path for missing files.
func getMutationQueueKey(filePath string) (string, error) {
	resolved, err := filepath.Abs(filePath)
	if err != nil {
		return "", err
	}
	realpath, err := filepath.EvalSymlinks(resolved)
	if err == nil {
		return realpath, nil
	}
	if os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR) {
		return resolved, nil
	}
	return "", err
}

type fileMutationQueue struct {
	mu    sync.Mutex
	users int // registered operations, including those waiting for mu
}

// WithFileMutationQueue serializes file mutation operations targeting the
// same file.
func WithFileMutationQueue(filePath string, fn func() error) error {
	key, err := getMutationQueueKey(filePath)
	if err != nil {
		return err
	}
	mutationQueuesMu.Lock()
	queue := mutationQueues[key]
	if queue == nil {
		queue = &fileMutationQueue{}
		mutationQueues[key] = queue
	}
	queue.users++
	mutationQueuesMu.Unlock()

	queue.mu.Lock()
	defer func() {
		queue.mu.Unlock()
		mutationQueuesMu.Lock()
		queue.users--
		if queue.users == 0 {
			delete(mutationQueues, key)
		}
		mutationQueuesMu.Unlock()
	}()
	return fn()
}
