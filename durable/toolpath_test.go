package durable

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/text/unicode/norm"
)

// Port of tools/path-utils.ts and tools/file-mutation-queue.ts.

func TestNormalizeToolPath(t *testing.T) {
	if got := NormalizeToolPath("\u00A0a\u3000b\u202Fc"); got != " a b c" {
		t.Fatalf("normalized = %q", got)
	}
	if got := NormalizeToolPath("@dir/file"); got != "dir/file" {
		t.Fatalf("prefix = %q", got)
	}
	if got := NormalizeToolPath("@@x"); got != "@x" {
		t.Fatalf("single prefix = %q", got)
	}
}

func TestResolveToolPath(t *testing.T) {
	fsys, dir := newTestFileSystem(t)
	ctx := context.Background()
	resolved, err := ResolveToolPath(ctx, fsys, "@a\u00A0b.txt")
	if err != nil || resolved != filepath.Join(dir, "a b.txt") {
		t.Fatalf("resolved = %q, %v", resolved, err)
	}
}

func TestResolveReadToolPathVariants(t *testing.T) {
	fsys, dir := newTestFileSystem(t)
	ctx := context.Background()
	// The real file carries a narrow no-break space before "AM.".
	narrow := filepath.Join(dir, "report 10\u202FAM.txt")
	if err := fsys.WriteFile(narrow, []byte("x"), ctx); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveReadToolPath(ctx, fsys, filepath.Join(dir, "report 10 AM.txt"))
	if err != nil || resolved != narrow {
		t.Fatalf("meridiem = %q, %v", resolved, err)
	}
	// NFD normalization.
	nfd := filepath.Join(dir, norm.NFD.String("caf\u00E9.txt"))
	if err := fsys.WriteFile(nfd, []byte("x"), ctx); err != nil {
		t.Fatal(err)
	}
	resolved, err = ResolveReadToolPath(ctx, fsys, filepath.Join(dir, "caf\u00E9.txt"))
	if err != nil || resolved != nfd {
		t.Fatalf("nfd = %q, %v", resolved, err)
	}
	// A typographic apostrophe.
	curly := filepath.Join(dir, "it\u2019s.txt")
	if err := fsys.WriteFile(curly, []byte("x"), ctx); err != nil {
		t.Fatal(err)
	}
	resolved, err = ResolveReadToolPath(ctx, fsys, filepath.Join(dir, "it's.txt"))
	if err != nil || resolved != curly {
		t.Fatalf("apostrophe = %q, %v", resolved, err)
	}
	// No variant exists: the plain resolution comes back.
	missing := filepath.Join(dir, "missing.txt")
	resolved, err = ResolveReadToolPath(ctx, fsys, missing)
	if err != nil || resolved != missing {
		t.Fatalf("missing = %q, %v", resolved, err)
	}
}

func TestWithFileMutationQueueSerializesOneFile(t *testing.T) {
	fsys, dir := newTestFileSystem(t)
	ctx := context.Background()
	path := filepath.Join(dir, "shared.txt")
	if err := fsys.WriteFile(path, []byte("0"), ctx); err != nil {
		t.Fatal(err)
	}
	inside := make(chan struct{}, 8)
	overlap := make(chan struct{}, 1)
	done := make(chan struct{}, 2)
	run := func() {
		_, _ = WithFileMutationQueue(ctx, fsys, path, func() (struct{}, error) {
			select {
			case inside <- struct{}{}:
			default:
				select {
				case overlap <- struct{}{}:
				default:
				}
			}
			time.Sleep(30 * time.Millisecond)
			<-inside
			return struct{}{}, nil
		})
		// Signal only after the queue released the key (the deferred cleanup
		// runs before WithFileMutationQueue returns).
		done <- struct{}{}
	}
	go run()
	time.Sleep(5 * time.Millisecond)
	go run()
	<-done
	<-done
	select {
	case <-overlap:
		t.Fatal("mutations of one file overlapped")
	default:
	}
	// The key is released once the queue drains.
	fileMutationQueues.mu.Lock()
	remaining := len(fileMutationQueues.keys)
	fileMutationQueues.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("queues = %d", remaining)
	}
}

func TestWithFileMutationQueueIndependentFiles(t *testing.T) {
	fsys, dir := newTestFileSystem(t)
	ctx := context.Background()
	first := filepath.Join(dir, "first.txt")
	second := filepath.Join(dir, "second.txt")
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, _ = WithFileMutationQueue(ctx, fsys, first, func() (struct{}, error) {
			close(started)
			<-release
			return struct{}{}, nil
		})
	}()
	<-started
	// A different file proceeds while the first is held.
	ran := make(chan struct{})
	go func() {
		_, _ = WithFileMutationQueue(ctx, fsys, second, func() (struct{}, error) {
			close(ran)
			return struct{}{}, nil
		})
	}()
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("a different file waited")
	}
	close(release)
}

func TestMutationKeyCanonicalizesMissingFile(t *testing.T) {
	fsys, dir := newTestFileSystem(t)
	ctx := context.Background()
	// A symlinked directory must not split the key of a not-yet-created file.
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	direct, err := mutationKey(ctx, fsys, filepath.Join(real, "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	viaLink, err := mutationKey(ctx, fsys, filepath.Join(link, "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if direct != viaLink {
		t.Fatalf("keys differ: %q vs %q", direct, viaLink)
	}
}
