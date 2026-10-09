package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Port of env/index.ts: the portable filesystem and shell capability.
//
// D188: upstream returns expected failures as a Result value; Go has the
// idiomatic (value, error) pair, so the port returns errors and the Result
// helpers here exist only for callers that need the upstream shape. FileError
// and ExecutionError keep the upstream codes.

// Result is the upstream fallible-operation result.
type Result[TValue any, TError any] struct {
	OK    bool
	Value TValue
	Err   TError
}

// ToError normalizes an unknown failure to an error.
func ToError(value any) error {
	switch typed := value.(type) {
	case nil:
		return nil
	case error:
		return typed
	case string:
		return errors.New(typed)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("%v", value)
	}
	return errors.New(string(encoded))
}

// FileKind is the kind of one filesystem entry.
type FileKind = string

// File kinds.
const (
	FileKindFile      FileKind = "file"
	FileKindDirectory FileKind = "directory"
	FileKindSymlink   FileKind = "symlink"
)

// FileErrorCode is a portable filesystem failure code.
type FileErrorCode = string

// File error codes.
const (
	FileErrorAborted          FileErrorCode = "aborted"
	FileErrorNotFound         FileErrorCode = "not_found"
	FileErrorPermissionDenied FileErrorCode = "permission_denied"
	FileErrorNotDirectory     FileErrorCode = "not_directory"
	FileErrorIsDirectory      FileErrorCode = "is_directory"
	FileErrorInvalid          FileErrorCode = "invalid"
	FileErrorNotSupported     FileErrorCode = "not_supported"
	FileErrorUnknown          FileErrorCode = "unknown"
)

// FileError is one portable filesystem failure.
type FileError struct {
	Code    FileErrorCode
	Message string
	Path    string
	Cause   error
}

func (e *FileError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s: %s", e.Path, e.Message)
	}
	return e.Message
}

func (e *FileError) Unwrap() error { return e.Cause }

// ExecutionErrorCode is a portable execution failure code.
type ExecutionErrorCode = string

// Execution error codes.
const (
	ExecutionErrorAborted          ExecutionErrorCode = "aborted"
	ExecutionErrorTimeout          ExecutionErrorCode = "timeout"
	ExecutionErrorShellUnavailable ExecutionErrorCode = "shell_unavailable"
	ExecutionErrorSpawn            ExecutionErrorCode = "spawn_error"
	ExecutionErrorCallback         ExecutionErrorCode = "callback_error"
	ExecutionErrorUnknown          ExecutionErrorCode = "unknown"
)

// ExecutionError is one portable execution failure.
type ExecutionError struct {
	Code    ExecutionErrorCode
	Message string
	// SpillPath is the spill file of a command that timed out or was aborted
	// after its output crossed the spill thresholds.
	SpillPath string
	Cause     error
}

func (e *ExecutionError) Error() string { return e.Message }
func (e *ExecutionError) Unwrap() error { return e.Cause }

// WatchTarget is a file or directory to watch; missing paths are watched for creation.
type WatchTarget struct {
	Path      string
	Recursive bool
	Exclude   *WatchExclude
}

// WatchExclude filters child entry names during watch scans.
type WatchExclude struct {
	Hidden bool
	Names  []string
}

// WatchChange reports paths that may have changed, uncertainty, or a terminal watcher error.
type WatchChange struct {
	// Paths lists changed file or directory paths.
	Paths []string
	// Overflow means watcher could not establish complete change coverage.
	Overflow bool
	// Error is terminal; no later changes are sent.
	Error *FileError
}

// WatchMode is how a filesystem watcher observes changes.
type WatchMode string

const WatchModePolling WatchMode = "polling"

// FileWatcher is one filesystem watch registration.
type FileWatcher interface {
	// Mode reports how the watcher observes changes.
	Mode() WatchMode
	// Close stops future notifications; it is idempotent.
	Close(ctx context.Context) error
}

// FileInfo describes one filesystem entry.
type FileInfo struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Kind    FileKind `json:"kind"`
	Size    int64    `json:"size"`
	MtimeMs float64  `json:"mtimeMs"`
}

// TextLine is one line with its terminator flag.
type TextLine struct {
	Text       string
	Terminated bool
}

// DirReader pages through one opened directory.
type DirReader interface {
	// Next returns up to maxEntries supported entries and whether listing is complete.
	Next(ctx context.Context, maxEntries int) ([]FileInfo, bool, error)
	// Close releases the directory handle; it is idempotent.
	Close(ctx context.Context) error
}

// TextLineReader streams lines without loading the whole file.
type TextLineReader interface {
	ReadLine(ctx context.Context) (*TextLine, error)
	Close(ctx context.Context) error
}

// ReadTextLinesOptions bounds a line read.
type ReadTextLinesOptions struct {
	MaxLines int
}

// CreateDirOptions controls directory creation.
type CreateDirOptions struct {
	Recursive bool
}

// RemoveOptions controls removal.
type RemoveOptions struct {
	Recursive bool
	Force     bool
}

// CreateTempFileOptions names a temporary file.
type CreateTempFileOptions struct {
	Prefix string
	Suffix string
}

// FileSystem is the portable filesystem capability.
type FileSystem interface {
	// ID is the file namespace: equal ids see the same files at the same
	// paths, whatever their cwd.
	ID() string
	Cwd() string
	SetCwd(path string)
	AbsolutePath(path string, ctx context.Context) (string, error)
	JoinPath(parts []string, ctx context.Context) (string, error)
	ReadTextFile(path string, ctx context.Context) (string, error)
	OpenBinaryReader(path string, ctx context.Context) (BinaryReader, error)
	OpenTextLineReader(path string, ctx context.Context) (TextLineReader, error)
	ReadTextLines(path string, options *ReadTextLinesOptions, ctx context.Context) ([]string, error)
	ReadBinaryFile(path string, ctx context.Context) ([]byte, error)
	WriteFile(path string, content []byte, ctx context.Context) error
	AppendFile(path string, content []byte, ctx context.Context) error
	TruncateFile(path string, size int64, ctx context.Context) error
	FlushFile(path string, ctx context.Context) error
	RenameFile(sourcePath string, destinationPath string, ctx context.Context) error
	FileInfo(path string, ctx context.Context) (*FileInfo, error)
	ListDir(path string, ctx context.Context) ([]FileInfo, error)
	// OpenDirReader opens one directory for paged listing.
	OpenDirReader(path string, ctx context.Context) (DirReader, error)
	// Watch observes changes to files and directories until the returned watcher closes.
	Watch(targets []WatchTarget, onChange func(WatchChange), ctx context.Context) (FileWatcher, error)
	CanonicalPath(path string, ctx context.Context) (string, error)
	Exists(path string, ctx context.Context) (bool, error)
	CreateDir(path string, options *CreateDirOptions, ctx context.Context) error
	Remove(path string, options *RemoveOptions, ctx context.Context) error
	CreateTempDir(prefix *string, ctx context.Context) (string, error)
	CreateTempFile(options *CreateTempFileOptions, ctx context.Context) (string, error)
	Cleanup(ctx context.Context) error
}

// ShellSpillOptions spills the complete output to a temporary file once it
// exceeds either threshold.
type ShellSpillOptions struct {
	AfterBytes int
	// AfterLines counts complete or partial lines.
	AfterLines int
}

// ShellExecResult is one finished command.
type ShellExecResult struct {
	ExitCode int
	// SpillPath is the temporary file holding the complete raw output, when
	// the spill thresholds were exceeded.
	SpillPath string
}

// ShellOutputInfo identifies the command stream and any output omitted before this chunk.
type ShellOutputInfo struct {
	// Stream is "stdout" or "stderr".
	Stream string
	// Skipped counts decoded text omitted immediately before this chunk.
	Skipped *ShellOutputSkip
}

// ShellOutputWindow describes the tail a caller retains and its progress pace.
type ShellOutputWindow struct {
	// MaxBytes and MaxLines bound retained decoded text.
	MaxBytes int
	MaxLines int
	// MinIntervalMs is the minimum pause between caller progress commits.
	MinIntervalMs int
	// BytesPerSecond adds a pause proportional to each progress commit's size.
	BytesPerSecond int
}

// ShellOutputSkip counts decoded text omitted immediately before an output chunk.
type ShellOutputSkip struct {
	// Bytes is the UTF-8 byte length of omitted text.
	Bytes int
	// Newlines counts U+000A characters in omitted text.
	Newlines int
	// EndsWithNewline records whether omitted text ends in U+000A.
	EndsWithNewline bool
}

// ShellExecOptions configures one command.
type ShellExecOptions struct {
	Cwd        string
	Env        map[string]string
	InheritEnv bool
	// Timeout is in seconds; nil selects no timeout (an explicit zero is
	// rejected, as upstream does).
	Timeout  *float64
	OnOutput func(text string, ctx context.Context, info ShellOutputInfo)
	Spill    *ShellSpillOptions
	// Window lets environments skip output outside a caller's retained tail.
	Window *ShellOutputWindow
}

// D214: Go exposes argv execution separately instead of weakening Exec's command type to any.
// Shell is the portable command capability.
type Shell interface {
	Exec(command string, options *ShellExecOptions, ctx context.Context) (ShellExecResult, error)
	ExecArgv(argv []string, options *ShellExecOptions, ctx context.Context) (ShellExecResult, error)
	Cleanup(ctx context.Context) error
}

// ExecutionEnv is a filesystem and shell capability.
type ExecutionEnv interface {
	FileSystem
	Shell
}
