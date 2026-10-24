package coding

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Reference: pi v1.4.0 core/tools/file-mutation-queue.ts deletes a path's
// queue after its last registered operation finishes, including thrown calls.
func TestFileMutationQueueReleasesFinishedPaths(t *testing.T) {
	mutationQueuesMu.Lock()
	before := len(mutationQueues)
	mutationQueuesMu.Unlock()
	want := errors.New("mutation failed")
	for _, name := range []string{"success", "failure", "panic"} {
		path := filepath.Join(t.TempDir(), name)
		func() {
			defer func() {
				if got := recover(); got != nil && got != want {
					t.Fatalf("panic=%v", got)
				}
			}()
			err := WithFileMutationQueue(path, func() error {
				switch name {
				case "failure":
					return want
				case "panic":
					panic(want)
				}
				return nil
			})
			if name == "failure" && !errors.Is(err, want) {
				t.Fatalf("error=%v", err)
			}
		}()
	}
	mutationQueuesMu.Lock()
	after := len(mutationQueues)
	mutationQueuesMu.Unlock()
	if after != before {
		t.Fatalf("finished paths retained: before=%d after=%d", before, after)
	}
}

// Reference: file-mutation-queue.ts rethrows realpath failures other than
// ENOENT/ENOTDIR, so the mutation must not execute through an invalid alias.
func TestFileMutationQueueRejectsSymlinkLoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loop")
	if err := os.Symlink("loop", path); err != nil {
		t.Skip(err)
	}
	called := false
	err := WithFileMutationQueue(path, func() error { called = true; return nil })
	if err == nil || called {
		t.Fatalf("loop error=%v mutation called=%v", err, called)
	}
}

// Reference: file-mutation-queue.ts resolves missing paths to absolute paths,
// so relative and absolute callers register against the same queue.
func TestFileMutationQueueCanonicalizesMissingRelativePath(t *testing.T) {
	absolute := filepath.Join(t.TempDir(), "not-created-yet")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, absolute)
	if err != nil {
		t.Fatal(err)
	}
	key, err := getMutationQueueKey(relative)
	if err != nil || key != absolute {
		t.Fatalf("key=%q err=%v want=%q", key, err, absolute)
	}
}
