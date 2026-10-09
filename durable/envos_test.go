package durable

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Port of the filesystem half of env/node.ts.

func newTestFileSystem(t *testing.T) (*OSFileSystem, string) {
	t.Helper()
	dir := t.TempDir()
	fsys, err := NewOSFileSystem(dir)
	if err != nil {
		t.Fatal(err)
	}
	return fsys, dir
}

func TestOSFileSystemPaths(t *testing.T) {
	fsys, dir := newTestFileSystem(t)
	ctx := context.Background()
	absolute, err := fsys.AbsolutePath("nested/file.txt", ctx)
	if err != nil || absolute != filepath.Join(dir, "nested", "file.txt") {
		t.Fatalf("absolute = %q, %v", absolute, err)
	}
	if fsys.ID() != "local" {
		t.Fatalf("id = %q", fsys.ID())
	}
	joined, err := fsys.JoinPath([]string{"a", "b", "c"}, ctx)
	if err != nil || joined != filepath.Join(dir, "a", "b", "c") {
		t.Fatalf("joined = %q, %v", joined, err)
	}
	home, _ := os.UserHomeDir()
	resolved, err := fsys.AbsolutePath("~", ctx)
	if err != nil || resolved != filepath.Clean(home) {
		t.Fatalf("tilde = %q, %v", resolved, err)
	}
	resolved, err = fsys.AbsolutePath("file:///tmp/x", ctx)
	if err != nil || !strings.HasSuffix(resolved, filepath.Join("tmp", "x")) {
		t.Fatalf("file url = %q, %v", resolved, err)
	}
}

func TestOSFileSystemReadWriteAndInfo(t *testing.T) {
	fsys, dir := newTestFileSystem(t)
	ctx := context.Background()
	path := filepath.Join(dir, "notes.txt")
	if err := fsys.WriteFile(path, []byte("hello\nworld"), ctx); err != nil {
		t.Fatal(err)
	}
	text, err := fsys.ReadTextFile(path, ctx)
	if err != nil || text != "hello\nworld" {
		t.Fatalf("text = %q, %v", text, err)
	}
	// Append and truncate.
	if err := fsys.AppendFile(path, []byte("!"), ctx); err != nil {
		t.Fatal(err)
	}
	binary, err := fsys.ReadBinaryFile(path, ctx)
	if err != nil || string(binary) != "hello\nworld!" {
		t.Fatalf("binary = %q, %v", binary, err)
	}
	if err := fsys.TruncateFile(path, 5, ctx); err != nil {
		t.Fatal(err)
	}
	text, _ = fsys.ReadTextFile(path, ctx)
	if text != "hello" {
		t.Fatalf("truncated = %q", text)
	}
	if err := fsys.FlushFile(path, ctx); err != nil {
		t.Fatal(err)
	}
	info, err := fsys.FileInfo(path, ctx)
	if err != nil || info.Kind != FileKindFile || info.Name != "notes.txt" || info.Size != 5 {
		t.Fatalf("info = %+v, %v", info, err)
	}
	if exists, _ := fsys.Exists(path, ctx); !exists {
		t.Fatal("file must exist")
	}
	if exists, _ := fsys.Exists(filepath.Join(dir, "missing"), ctx); exists {
		t.Fatal("missing path must not exist")
	}
	// Lines.
	multi := filepath.Join(dir, "lines.txt")
	if err := fsys.WriteFile(multi, []byte("a\nb\nc"), ctx); err != nil {
		t.Fatal(err)
	}
	lines, err := fsys.ReadTextLines(multi, nil, ctx)
	if err != nil || len(lines) != 3 || lines[0] != "a" || lines[2] != "c" {
		t.Fatalf("lines = %v, %v", lines, err)
	}
	limited, err := fsys.ReadTextLines(multi, &ReadTextLinesOptions{MaxLines: 2}, ctx)
	if err != nil || len(limited) != 2 {
		t.Fatalf("limited = %v, %v", limited, err)
	}
	reader, err := fsys.OpenTextLineReader(multi, ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := reader.ReadLine(ctx)
	if err != nil || first == nil || first.Text != "a" || !first.Terminated {
		t.Fatalf("first = %+v, %v", first, err)
	}
	_ = reader.Close(ctx)
}

func TestOSFileSystemDirsAndRename(t *testing.T) {
	fsys, dir := newTestFileSystem(t)
	ctx := context.Background()
	nested := filepath.Join(dir, "a", "b")
	if err := fsys.CreateDir(nested, &CreateDirOptions{Recursive: true}, ctx); err != nil {
		t.Fatal(err)
	}
	if err := fsys.CreateDir(nested, nil, ctx); err == nil {
		t.Fatal("creating an existing directory without recursive must fail")
	}
	source := filepath.Join(nested, "one.txt")
	if err := fsys.WriteFile(source, []byte("one"), ctx); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(nested, "two.txt")
	if err := fsys.RenameFile(source, destination, ctx); err != nil {
		t.Fatal(err)
	}
	infos, err := fsys.ListDir(nested, ctx)
	if err != nil || len(infos) != 1 || infos[0].Name != "two.txt" {
		t.Fatalf("list = %+v, %v", infos, err)
	}
	canonical, err := fsys.CanonicalPath(destination, ctx)
	if err != nil || !strings.HasSuffix(canonical, "two.txt") {
		t.Fatalf("canonical = %q, %v", canonical, err)
	}
	if err := fsys.Remove(nested, &RemoveOptions{Recursive: true}, ctx); err != nil {
		t.Fatal(err)
	}
	if exists, _ := fsys.Exists(nested, ctx); exists {
		t.Fatal("recursive remove must delete the tree")
	}
	// Force tolerates a missing path.
	if err := fsys.Remove(filepath.Join(dir, "missing"), &RemoveOptions{Force: true}, ctx); err != nil {
		t.Fatalf("force remove = %v", err)
	}
}

// Port of FileSystem.openDirReader paging from packages/durable/src/env/node.ts at pi v1.1.0 commit 4748c627a.
func TestOSFileSystemDirectoryReaderPagesEntries(t *testing.T) {
	fsys, _ := newTestFileSystem(t)
	ctx := context.Background()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := fsys.WriteFile(name, []byte(name), ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := fsys.CreateDir("nested", nil, ctx); err != nil {
		t.Fatal(err)
	}
	reader, err := fsys.OpenDirReader(".", ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(ctx)
	if _, _, err := reader.Next(ctx, 0); err == nil {
		t.Fatal("non-positive page size must fail")
	} else {
		var fileError *FileError
		if !errors.As(err, &fileError) || fileError.Code != FileErrorInvalid {
			t.Fatalf("page-size error = %v", err)
		}
	}
	seen := map[string]bool{}
	for {
		entries, done, err := reader.Next(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) > 2 || (!done && len(entries) == 0) {
			t.Fatalf("page = %+v, done=%t", entries, done)
		}
		for _, entry := range entries {
			seen[entry.Name] = true
			if entry.Path == "" || entry.Name == "" {
				t.Fatalf("entry metadata = %+v", entry)
			}
		}
		if done {
			break
		}
	}
	for _, name := range []string{"alpha", "beta", "gamma", "nested"} {
		if !seen[name] {
			t.Fatalf("directory listing omitted %q: %+v", name, seen)
		}
	}
	if err := reader.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reader.Next(ctx, 1); err == nil {
		t.Fatal("closed directory reader rejects next")
	}
}

func TestOSFileSystemErrorCodes(t *testing.T) {
	fsys, dir := newTestFileSystem(t)
	ctx := context.Background()
	if _, err := fsys.ReadTextFile(filepath.Join(dir, "missing"), ctx); err == nil {
		t.Fatal("missing file must fail")
	} else {
		var fileError *FileError
		if !errors.As(err, &fileError) || fileError.Code != FileErrorNotFound {
			t.Fatalf("code = %+v", err)
		}
	}
	// A file where a directory is expected.
	notDir := filepath.Join(dir, "plain")
	if err := fsys.WriteFile(notDir, []byte("x"), ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := fsys.ListDir(notDir, ctx); err == nil {
		t.Fatal("listing a file must fail")
	} else {
		var fileError *FileError
		if !errors.As(err, &fileError) || fileError.Code != FileErrorNotDirectory {
			t.Fatalf("code = %+v", err)
		}
	}
	// A directory where a file is expected.
	if _, err := fsys.ReadTextFile(dir, ctx); err == nil {
		t.Fatal("reading a directory must fail")
	} else {
		var fileError *FileError
		if !errors.As(err, &fileError) || fileError.Code != FileErrorIsDirectory {
			t.Fatalf("code = %+v", err)
		}
	}
}

func TestOSFileSystemTempAndCleanup(t *testing.T) {
	// Temporary files must land in the test's own directory, never the shared
	// temp dir.
	t.Setenv("TMPDIR", t.TempDir())
	fsys, _ := newTestFileSystem(t)
	ctx := context.Background()
	prefix := "pier-test-"
	dir, err := fsys.CreateTempDir(&prefix, ctx)
	if err != nil {
		t.Fatal(err)
	}
	file, err := fsys.CreateTempFile(&CreateTempFileOptions{Prefix: "pier-", Suffix: ".log"}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := fsys.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if exists, _ := fsys.Exists(dir, ctx); exists {
		t.Fatal("temp dir survived cleanup")
	}
	if exists, _ := fsys.Exists(file, ctx); exists {
		t.Fatal("temp file survived cleanup")
	}
}
