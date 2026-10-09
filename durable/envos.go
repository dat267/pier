package durable

import (
	"bufio"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Port of env/node.ts's filesystem half: the local Node environment over the
// operating system's files.
//
// D188 applies: failures are returned as *FileError values (codes preserved)
// rather than upstream's Result wrapper.

// OSFileSystem is the local filesystem capability.
type OSFileSystem struct {
	mu                  sync.Mutex
	cwd                 string
	tempDirs            []string
	tempFiles           []string
	watchPollInterval   time.Duration
	maxWatchDirectories int
}

// NewOSFileSystem builds the local filesystem capability over one working
// directory (empty selects the process working directory).
func NewOSFileSystem(cwd string) (*OSFileSystem, error) {
	if cwd == "" {
		current, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cwd = current
	}
	return &OSFileSystem{cwd: cwd, watchPollInterval: 2 * time.Second, maxWatchDirectories: 10_000}, nil
}

// ID is the shared local namespace.
func (e *OSFileSystem) ID() string { return "local" }

// Cwd is the current working directory.
func (e *OSFileSystem) Cwd() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cwd
}

// SetCwd replaces the working directory.
func (e *OSFileSystem) SetCwd(path string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cwd = path
}

// AbsolutePath resolves a path against the working directory.
func (e *OSFileSystem) AbsolutePath(path string, ctx context.Context) (string, error) {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

// JoinPath joins path segments.
func (e *OSFileSystem) JoinPath(parts []string, ctx context.Context) (string, error) {
	if len(parts) == 0 {
		return e.Cwd(), nil
	}
	resolved, err := e.resolvePath(parts[0])
	if err != nil {
		return "", err
	}
	joined := filepath.Join(append([]string{resolved}, parts[1:]...)...)
	return joined, nil
}

// ReadTextFile reads a whole UTF-8 file.
func (e *OSFileSystem) ReadTextFile(path string, ctx context.Context) (string, error) {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return "", toFileError(err, resolved)
	}
	return string(content), nil
}

// ReadBinaryFile reads a whole file.
func (e *OSFileSystem) ReadBinaryFile(path string, ctx context.Context) ([]byte, error) {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return nil, toFileError(err, resolved)
	}
	return content, nil
}

// OpenTextLineReader streams a file line by line.
func (e *OSFileSystem) OpenTextLineReader(path string, ctx context.Context) (TextLineReader, error) {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, toFileError(err, resolved)
	}
	return &osTextLineReader{path: resolved, file: file, reader: bufio.NewReader(file)}, nil
}

// ReadTextLines reads up to options.MaxLines lines (all when nil).
func (e *OSFileSystem) ReadTextLines(path string, options *ReadTextLinesOptions, ctx context.Context) ([]string, error) {
	reader, err := e.OpenTextLineReader(path, ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close(ctx) }()
	max := -1
	if options != nil && options.MaxLines > 0 {
		max = options.MaxLines
	}
	var lines []string
	for max < 0 || len(lines) < max {
		line, err := reader.ReadLine(ctx)
		if err != nil {
			return nil, err
		}
		if line == nil {
			break
		}
		lines = append(lines, line.Text)
	}
	return lines, nil
}

// WriteFile writes a whole file.
func (e *OSFileSystem) WriteFile(path string, content []byte, ctx context.Context) error {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(resolved, content, 0o644); err != nil {
		return toFileError(err, resolved)
	}
	return nil
}

// AppendFile appends to a file.
func (e *OSFileSystem) AppendFile(path string, content []byte, ctx context.Context) error {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(resolved, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return toFileError(err, resolved)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(content); err != nil {
		return toFileError(err, resolved)
	}
	return nil
}

// TruncateFile truncates or extends a file to exactly size bytes.
func (e *OSFileSystem) TruncateFile(path string, size int64, ctx context.Context) error {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return err
	}
	if err := os.Truncate(resolved, size); err != nil {
		return toFileError(err, resolved)
	}
	return nil
}

// FlushFile flushes file contents and metadata.
func (e *OSFileSystem) FlushFile(path string, ctx context.Context) error {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(resolved, os.O_RDWR, 0)
	if err != nil {
		return toFileError(err, resolved)
	}
	defer func() { _ = file.Close() }()
	if err := file.Sync(); err != nil {
		return toFileError(err, resolved)
	}
	return nil
}

// RenameFile renames a file or directory.
func (e *OSFileSystem) RenameFile(sourcePath string, destinationPath string, ctx context.Context) error {
	source, err := e.resolvePath(sourcePath)
	if err != nil {
		return err
	}
	destination, err := e.resolvePath(destinationPath)
	if err != nil {
		return err
	}
	if err := os.Rename(source, destination); err != nil {
		return toFileError(err, source)
	}
	return nil
}

// FileInfo stats one entry.
func (e *OSFileSystem) FileInfo(path string, ctx context.Context) (*FileInfo, error) {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return nil, toFileError(err, resolved)
	}
	result, err := fileInfoFromStat(resolved, info)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ListDir lists one directory.
func (e *OSFileSystem) ListDir(path string, ctx context.Context) ([]FileInfo, error) {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, toFileError(err, resolved)
	}
	infos := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, toFileError(err, filepath.Join(resolved, entry.Name()))
		}
		converted, err := fileInfoFromStat(filepath.Join(resolved, entry.Name()), info)
		if err != nil {
			return nil, err
		}
		infos = append(infos, *converted)
	}
	return infos, nil
}

// OpenDirReader opens a paged directory listing.
func (e *OSFileSystem) OpenDirReader(path string, ctx context.Context) (DirReader, error) {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return nil, err
	}
	if ctx != nil && ctx.Err() != nil {
		return nil, toFileError(ctx.Err(), resolved)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, toFileError(err, resolved)
	}
	if !info.IsDir() {
		return nil, &FileError{Code: FileErrorNotDirectory, Message: "Not a directory", Path: resolved}
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, toFileError(err, resolved)
	}
	opened, err := file.Stat()
	if err != nil || !opened.IsDir() {
		_ = file.Close()
		if err != nil {
			return nil, toFileError(err, resolved)
		}
		return nil, &FileError{Code: FileErrorNotDirectory, Message: "Not a directory", Path: resolved}
	}
	return &osDirReader{file: file, path: resolved}, nil
}

// CanonicalPath resolves symlinks.
func (e *OSFileSystem) CanonicalPath(path string, ctx context.Context) (string, error) {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", toFileError(err, resolved)
	}
	return canonical, nil
}

// Exists reports whether a path exists.
func (e *OSFileSystem) Exists(path string, ctx context.Context) (bool, error) {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return false, err
	}
	_, statErr := os.Lstat(resolved)
	if statErr == nil {
		return true, nil
	}
	if errors.Is(statErr, fs.ErrNotExist) {
		return false, nil
	}
	return false, toFileError(statErr, resolved)
}

// CreateDir creates a directory.
func (e *OSFileSystem) CreateDir(path string, options *CreateDirOptions, ctx context.Context) error {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return err
	}
	recursive := options != nil && options.Recursive
	if recursive {
		if err := os.MkdirAll(resolved, 0o755); err != nil {
			return toFileError(err, resolved)
		}
		return nil
	}
	if err := os.Mkdir(resolved, 0o755); err != nil {
		return toFileError(err, resolved)
	}
	return nil
}

// Remove removes a file or directory.
func (e *OSFileSystem) Remove(path string, options *RemoveOptions, ctx context.Context) error {
	resolved, err := e.resolvePath(path)
	if err != nil {
		return err
	}
	recursive := options != nil && options.Recursive
	force := options != nil && options.Force
	if !recursive {
		if err := os.Remove(resolved); err != nil {
			if force && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return toFileError(err, resolved)
		}
		return nil
	}
	if err := os.RemoveAll(resolved); err != nil {
		return toFileError(err, resolved)
	}
	return nil
}

// CreateTempDir creates a tracked temporary directory.
func (e *OSFileSystem) CreateTempDir(prefix *string, ctx context.Context) (string, error) {
	name := "durable-"
	if prefix != nil && *prefix != "" {
		name = *prefix
	}
	dir, err := os.MkdirTemp("", name)
	if err != nil {
		return "", toFileError(err, "")
	}
	e.mu.Lock()
	e.tempDirs = append(e.tempDirs, dir)
	e.mu.Unlock()
	return dir, nil
}

// CreateTempFile creates a tracked temporary file.
func (e *OSFileSystem) CreateTempFile(options *CreateTempFileOptions, ctx context.Context) (string, error) {
	prefix := "durable-"
	suffix := ""
	if options != nil {
		if options.Prefix != "" {
			prefix = options.Prefix
		}
		suffix = options.Suffix
	}
	file, err := os.CreateTemp("", prefix+"*"+suffix)
	if err != nil {
		return "", toFileError(err, "")
	}
	name := file.Name()
	_ = file.Close()
	e.mu.Lock()
	e.tempFiles = append(e.tempFiles, name)
	e.mu.Unlock()
	return name, nil
}

// Cleanup removes every temporary directory and file this environment created.
func (e *OSFileSystem) Cleanup(ctx context.Context) error {
	e.mu.Lock()
	dirs := e.tempDirs
	files := e.tempFiles
	e.tempDirs = nil
	e.tempFiles = nil
	e.mu.Unlock()
	var firstError error
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil && firstError == nil {
			firstError = toFileError(err, dir)
		}
	}
	for _, file := range files {
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) && firstError == nil {
			firstError = toFileError(err, file)
		}
	}
	return firstError
}

// resolvePath normalizes ~, file URLs and relative paths against the cwd.
func (e *OSFileSystem) resolvePath(path string) (string, error) {
	normalized := path
	home, _ := os.UserHomeDir()
	switch {
	case normalized == "~":
		normalized = home
	case strings.HasPrefix(normalized, "~/"):
		normalized = filepath.Join(home, normalized[2:])
	case strings.HasPrefix(normalized, "file://"):
		if parsed, err := url.Parse(normalized); err == nil && parsed.Path != "" {
			normalized = filepath.FromSlash(parsed.Path)
		}
	}
	if !filepath.IsAbs(normalized) {
		normalized = filepath.Join(e.Cwd(), normalized)
	}
	return filepath.Clean(normalized), nil
}

// fileInfoFromStat converts one stat result.
func fileInfoFromStat(path string, info fs.FileInfo) (*FileInfo, error) {
	kind := ""
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		kind = FileKindSymlink
	case info.IsDir():
		kind = FileKindDirectory
	case info.Mode().IsRegular():
		kind = FileKindFile
	default:
		return nil, &FileError{Code: FileErrorInvalid, Message: "Unsupported file type", Path: path}
	}
	return &FileInfo{
		Name: filepath.Base(path), Path: path, Kind: kind, Size: info.Size(),
		MtimeMs: float64(info.ModTime().UnixNano()) / float64(time.Millisecond),
	}, nil
}

// toFileError maps an operating-system error to a portable code.
func toFileError(err error, fallbackPath string) error {
	if err == nil {
		return nil
	}
	var fileError *FileError
	if errors.As(err, &fileError) {
		return fileError
	}
	path := fallbackPath
	var pathError *fs.PathError
	if errors.As(err, &pathError) && pathError.Path != "" {
		path = pathError.Path
	}
	code := FileErrorUnknown
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code = FileErrorAborted
	case errors.Is(err, fs.ErrNotExist):
		code = FileErrorNotFound
	case errors.Is(err, fs.ErrPermission):
		code = FileErrorPermissionDenied
	case errors.Is(err, syscall.ENOTDIR):
		code = FileErrorNotDirectory
	case errors.Is(err, syscall.EISDIR):
		code = FileErrorIsDirectory
	case errors.Is(err, fs.ErrInvalid):
		code = FileErrorInvalid
	}
	return &FileError{Code: code, Message: err.Error(), Path: path, Cause: err}
}

type osDirReader struct {
	file   *os.File
	path   string
	closed bool
}

func (r *osDirReader) Next(ctx context.Context, maxEntries int) ([]FileInfo, bool, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, false, toFileError(ctx.Err(), r.path)
	}
	if r.closed {
		return nil, false, &FileError{Code: FileErrorInvalid, Message: "Directory reader is closed", Path: r.path}
	}
	if maxEntries <= 0 {
		return nil, false, &FileError{Code: FileErrorInvalid, Message: "maxEntries must be positive", Path: r.path}
	}
	entries, err := r.file.ReadDir(maxEntries)
	done := errors.Is(err, io.EOF)
	if err != nil && !done {
		return nil, false, toFileError(err, r.path)
	}
	infos := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		if ctx != nil && ctx.Err() != nil {
			return nil, false, toFileError(ctx.Err(), r.path)
		}
		path := filepath.Join(r.path, entry.Name())
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, toFileError(err, path)
		}
		if !info.IsDir() && !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		converted, err := fileInfoFromStat(path, info)
		if err != nil {
			return nil, false, err
		}
		infos = append(infos, *converted)
	}
	return infos, done, nil
}

func (r *osDirReader) Close(_ context.Context) error {
	if r.closed {
		return nil
	}
	r.closed = true
	if err := r.file.Close(); err != nil {
		return toFileError(err, r.path)
	}
	return nil
}

// osTextLineReader streams one open file.
type osTextLineReader struct {
	path       string
	file       *os.File
	reader     *bufio.Reader
	terminated bool
	closed     bool
}

func (r *osTextLineReader) ReadLine(ctx context.Context) (*TextLine, error) {
	if r.closed {
		return nil, &FileError{Code: FileErrorInvalid, Message: "Text line reader is closed", Path: r.path}
	}
	line, err := r.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, toFileError(err, r.path)
	}
	if line == "" && errors.Is(err, io.EOF) {
		return nil, nil
	}
	terminated := strings.HasSuffix(line, "\n")
	if terminated {
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")
	}
	return &TextLine{Text: line, Terminated: terminated}, nil
}

func (r *osTextLineReader) Close(ctx context.Context) error {
	if r.closed {
		return nil
	}
	r.closed = true
	if err := r.file.Close(); err != nil {
		return toFileError(err, r.path)
	}
	return nil
}

var _ FileSystem = (*OSFileSystem)(nil)
