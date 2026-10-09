package durable

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Port of the shell half of env/node.ts: the local command capability.
//
// The Go port runs argv directly when requested and reads stdout/stderr from
// separate pipes. A bounded event channel multiplexes chunks for the callback
// and spill writer, so cross-stream order follows goroutine send order rather
// than a shared OS pipe's byte order (D188).

const (
	// maxTimeoutMs is upstream MAX_TIMEOUT_MS.
	maxTimeoutMs = 2_147_483_647

	// exitStdioGrace is upstream EXIT_STDIO_GRACE_MS: after the process exits,
	// output already in the pipe is still drained for this long before a
	// descendant holding the write end is abandoned.
	exitStdioGrace = 100 * time.Millisecond
)

// OSShell executes commands through the local shell.
type OSShell struct {
	mu        sync.Mutex
	cwd       string
	shellPath string
	shellEnv  map[string]string
	spill     []string
}

// NewOSShell builds the local shell capability (empty cwd selects the process
// working directory).
func NewOSShell(cwd string, shellPath string, shellEnv map[string]string) (*OSShell, error) {
	if cwd == "" {
		current, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cwd = current
	}
	return &OSShell{cwd: cwd, shellPath: shellPath, shellEnv: shellEnv}, nil
}

// Cwd is the shell's working directory.
func (s *OSShell) Cwd() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd
}

// resolveTimeoutMs validates a timeout in seconds (upstream resolveTimeoutMs).
func resolveTimeoutMs(timeout *float64) (time.Duration, bool, error) {
	if timeout == nil {
		return 0, false, nil
	}
	seconds := *timeout
	if seconds != seconds /* NaN */ || seconds <= 0 {
		return 0, false, &ExecutionError{Code: ExecutionErrorTimeout, Message: "Invalid timeout: must be a finite number of seconds"}
	}
	millis := seconds * 1000
	if millis > maxTimeoutMs {
		return 0, false, &ExecutionError{
			Code:    ExecutionErrorTimeout,
			Message: fmt.Sprintf("Invalid timeout: maximum is %s seconds", strconv.FormatFloat(float64(maxTimeoutMs)/1000, 'f', -1, 64)),
		}
	}
	return time.Duration(millis) * time.Millisecond, true, nil
}

// Exec runs one command through the shell.
func (s *OSShell) Exec(command string, options *ShellExecOptions, ctx context.Context) (ShellExecResult, error) {
	return s.execCommand(command, nil, false, options, ctx)
}

// ExecArgv runs a program directly with arguments, without shell parsing.
func (s *OSShell) ExecArgv(argv []string, options *ShellExecOptions, ctx context.Context) (ShellExecResult, error) {
	if len(argv) == 0 || argv[0] == "" {
		return ShellExecResult{}, &ExecutionError{Code: ExecutionErrorSpawn, Message: "Empty argv: no program to run"}
	}
	return s.execCommand("", argv, true, options, ctx)
}

func (s *OSShell) execCommand(command string, argv []string, direct bool, options *ShellExecOptions, ctx context.Context) (ShellExecResult, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return ShellExecResult{}, &ExecutionError{Code: ExecutionErrorAborted, Message: "aborted", Cause: err}
		}
	}
	var timeout *float64
	if options != nil {
		timeout = options.Timeout
	}
	timeoutDuration, hasTimeout, err := resolveTimeoutMs(timeout)
	if err != nil {
		return ShellExecResult{}, err
	}
	s.mu.Lock()
	cwd := s.cwd
	s.mu.Unlock()
	if options != nil && options.Cwd != "" {
		cwd = filepath.Clean(filepath.Join(cwd, options.Cwd))
		if filepath.IsAbs(options.Cwd) {
			cwd = filepath.Clean(options.Cwd)
		}
	}
	var program string
	var commandArgs []string
	stdinCommand := false
	if direct {
		program = argv[0]
		commandArgs = argv[1:]
	} else {
		shell, args, stdinTransport, err := s.resolveShell()
		if err != nil {
			return ShellExecResult{}, err
		}
		program = shell
		commandArgs = args
		if !stdinTransport {
			commandArgs = append(append([]string{}, args...), command)
		} else {
			stdinCommand = true
		}
	}
	if _, statErr := os.Stat(cwd); statErr != nil {
		return ShellExecResult{}, &ExecutionError{
			Code: ExecutionErrorSpawn, Cause: toFileError(statErr, cwd),
			Message: fmt.Sprintf("Working directory does not exist: %s\nCannot execute bash commands.", cwd),
		}
	}

	cmd := exec.Command(program, commandArgs...)
	cmd.Dir = cwd
	cmd.Env = shellEnv(s.shellEnv, options)
	configureProcessGroup(cmd)
	stdoutReader, stdoutWriter, pipeErr := os.Pipe()
	if pipeErr != nil {
		return ShellExecResult{}, &ExecutionError{Code: ExecutionErrorSpawn, Message: pipeErr.Error(), Cause: pipeErr}
	}
	stderrReader, stderrWriter, pipeErr := os.Pipe()
	if pipeErr != nil {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		return ShellExecResult{}, &ExecutionError{Code: ExecutionErrorSpawn, Message: pipeErr.Error(), Cause: pipeErr}
	}
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	if stdinCommand {
		cmd.Stdin = strings.NewReader(command)
	}
	if startErr := cmd.Start(); startErr != nil {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
		return ShellExecResult{}, &ExecutionError{Code: ExecutionErrorSpawn, Message: startErr.Error(), Cause: startErr}
	}
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()

	// Kill the tree on timeout or caller cancellation.
	interrupted := make(chan struct{}, 1)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	timedOut := false
	var interruptMu sync.Mutex
	stopWatcher := make(chan struct{})
	go func() {
		var timeoutChannel <-chan time.Time
		if hasTimeout {
			timer.Reset(timeoutDuration)
			timeoutChannel = timer.C
		}
		var done <-chan struct{}
		if ctx != nil {
			done = ctx.Done()
		}
		select {
		case <-timeoutChannel:
			interruptMu.Lock()
			timedOut = true
			interruptMu.Unlock()
			select {
			case interrupted <- struct{}{}:
			default:
			}
		case <-done:
			select {
			case interrupted <- struct{}{}:
			default:
			}
		case <-stopWatcher:
		}
	}()

	var spillOptions *ShellSpillOptions
	if options != nil {
		spillOptions = options.Spill
	}
	spillState := &shellSpillState{options: spillOptions}
	type outputChunk struct {
		stream string
		bytes  []byte
	}
	chunks := make(chan outputChunk, 8)
	pumpDone := make(chan struct{}, 2)
	pump := func(reader *os.File, stream string) {
		go func() {
			buffer := make([]byte, 32*1024)
			for {
				count, readErr := reader.Read(buffer)
				if count > 0 {
					chunk := append([]byte(nil), buffer[:count]...)
					chunks <- outputChunk{stream: stream, bytes: chunk}
				}
				if readErr != nil {
					break
				}
			}
			pumpDone <- struct{}{}
		}()
	}
	pump(stdoutReader, "stdout")
	pump(stderrReader, "stderr")
	stdoutDecoder, stderrDecoder := &utf8ChunkReader{}, &utf8ChunkReader{}
	// Like upstream env/node.ts, OSShell emits every chunk; Window is for environments that can omit text safely.
	var callbackError error
	var spillError error
	emit := func(text, stream string) {
		if text == "" || callbackError != nil || options == nil || options.OnOutput == nil {
			return
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				cause := ToError(recovered)
				callbackError = &ExecutionError{Code: ExecutionErrorCallback, Message: cause.Error(), Cause: cause}
			}
		}()
		options.OnOutput(text, ctx, ShellOutputInfo{Stream: stream})
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	var waitErr error
	waitChannel := (<-chan error)(waitDone)
	interruptedChannel := (<-chan struct{})(interrupted)
	var graceTimer *time.Timer
	var graceChannel <-chan time.Time
	completedPumps := 0
	for {
		if waitChannel == nil && completedPumps == 2 && len(chunks) == 0 {
			break
		}
		select {
		case chunk := <-chunks:
			decoder := stdoutDecoder
			if chunk.stream == "stderr" {
				decoder = stderrDecoder
			}
			emit(decoder.decode(chunk.bytes), chunk.stream)
			if callbackError == nil && spillError == nil {
				if err := s.appendSpill(spillState, chunk.bytes); err != nil {
					spillError = err
				}
			}
		case <-pumpDone:
			completedPumps++
		case waitErr = <-waitChannel:
			waitChannel = nil
			graceTimer = time.NewTimer(exitStdioGrace)
			graceChannel = graceTimer.C
		case <-interruptedChannel:
			interruptedChannel = nil
			killProcessTree(cmd)
		case <-graceChannel:
			graceChannel = nil
			_ = stdoutReader.Close()
			_ = stderrReader.Close()
		}
	}
	if graceTimer != nil {
		graceTimer.Stop()
	}
	_ = stdoutReader.Close()
	_ = stderrReader.Close()
	emit(stdoutDecoder.decode(nil), "stdout")
	emit(stderrDecoder.decode(nil), "stderr")
	close(stopWatcher)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	if callbackError != nil {
		return ShellExecResult{}, callbackError
	}
	if spillError != nil {
		return ShellExecResult{}, spillError
	}
	result := ShellExecResult{ExitCode: 0}
	if waitErr != nil {
		var exitError *exec.ExitError
		if errors.As(waitErr, &exitError) {
			result.ExitCode = exitError.ExitCode()
		} else {
			return result, &ExecutionError{Code: ExecutionErrorSpawn, Message: waitErr.Error(), Cause: waitErr}
		}
	}
	interruptMu.Lock()
	wasTimedOut := timedOut
	interruptMu.Unlock()
	if wasTimedOut || (ctx != nil && ctx.Err() != nil) {
		executionError := &ExecutionError{Code: ExecutionErrorAborted, Message: "aborted"}
		if wasTimedOut && timeout != nil {
			executionError = &ExecutionError{Code: ExecutionErrorTimeout, Message: fmt.Sprintf("timeout:%g", *timeout)}
		}
		executionError.SpillPath = spillState.path
		return ShellExecResult{}, executionError
	}
	result.SpillPath = spillState.path
	return result, nil
}

// Cleanup removes every spill file this shell created.
func (s *OSShell) Cleanup(ctx context.Context) error {
	s.mu.Lock()
	spills := s.spill
	s.spill = nil
	s.mu.Unlock()
	var firstError error
	for _, path := range spills {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) && firstError == nil {
			firstError = err
		}
	}
	return firstError
}

// appendSpill counts one chunk against the spill thresholds and writes to the
// spill file once either is crossed.
func (s *OSShell) appendSpill(state *shellSpillState, chunk []byte) error {
	if state.options == nil || len(chunk) == 0 {
		return nil
	}
	if state.file != nil {
		return state.write(chunk)
	}
	state.seenBytes += len(chunk)
	for index, value := range chunk {
		if value == '\n' {
			state.seenNewlines++
		}
		_ = index
	}
	lines := state.seenNewlines
	if chunk[len(chunk)-1] != '\n' {
		lines++
	}
	if state.seenBytes <= state.options.AfterBytes && lines <= state.options.AfterLines {
		state.prefix = append(state.prefix, append([]byte{}, chunk...))
		return nil
	}
	if state.file == nil {
		path, err := s.createSpillFile()
		if err != nil {
			return &ExecutionError{Code: ExecutionErrorUnknown, Message: fmt.Sprintf("Failed to preserve complete shell output: %v", err), Cause: err}
		}
		state.path = path
	}
	for _, buffered := range state.prefix {
		if err := state.write(buffered); err != nil {
			return err
		}
	}
	state.prefix = nil
	return state.write(chunk)
}

// createSpillFile creates one tracked spill file.
func (s *OSShell) createSpillFile() (string, error) {
	file, err := os.CreateTemp("", "pi-output-*.log")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return "", err
	}
	s.mu.Lock()
	s.spill = append(s.spill, path)
	s.mu.Unlock()
	return path, nil
}

// shellSpillState accumulates the prefix until a threshold is crossed.
type shellSpillState struct {
	options      *ShellSpillOptions
	prefix       [][]byte
	seenBytes    int
	seenNewlines int
	path         string
	file         *os.File
}

func (state *shellSpillState) write(chunk []byte) error {
	if state.file == nil {
		file, err := os.OpenFile(state.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return &ExecutionError{Code: ExecutionErrorUnknown, Message: fmt.Sprintf("Failed to preserve complete shell output: %v", err), Cause: err}
		}
		state.file = file
	}
	if _, err := state.file.Write(chunk); err != nil {
		return &ExecutionError{Code: ExecutionErrorUnknown, Message: fmt.Sprintf("Failed to preserve complete shell output: %v", err), Cause: err}
	}
	return nil
}

// resolveShell selects the shell program (upstream getShellConfig).
func (s *OSShell) resolveShell() (string, []string, bool, error) {
	if s.shellPath != "" {
		if _, err := os.Stat(s.shellPath); err != nil {
			return "", nil, false, &ExecutionError{Code: ExecutionErrorShellUnavailable,
				Message: fmt.Sprintf("Custom shell path not found: %s", s.shellPath)}
		}
		return s.shellPath, bashArgs(s.shellPath), isLegacyWSLBashPath(s.shellPath), nil
	}
	if runtime.GOOS == "windows" {
		var candidates []string
		if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
			candidates = append(candidates, filepath.Join(programFiles, "Git", "bin", "bash.exe"))
		}
		if programFilesX86 := os.Getenv("ProgramFiles(x86)"); programFilesX86 != "" {
			candidates = append(candidates, filepath.Join(programFilesX86, "Git", "bin", "bash.exe"))
		}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				return candidate, bashArgs(candidate), isLegacyWSLBashPath(candidate), nil
			}
		}
		if found, err := exec.LookPath("bash"); err == nil {
			return found, bashArgs(found), false, nil
		}
		return "", nil, false, &ExecutionError{Code: ExecutionErrorShellUnavailable,
			Message: "No bash shell found. Options:\n" +
				"  1. Install Git for Windows: https://git-scm.com/download/win\n" +
				"  2. Add your bash to PATH (Cygwin, MSYS2, etc.)\n" +
				"  3. Configure an explicit shellPath\n\n" +
				"Searched Git Bash in:\n" + strings.Join(prefixAll(candidates), "\n")}
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash", bashArgs("/bin/bash"), false, nil
	}
	if found, err := exec.LookPath("bash"); err == nil {
		return found, bashArgs(found), false, nil
	}
	return "sh", []string{"-c"}, false, nil
}

// bashArgs is upstream getBashShellConfig.
func bashArgs(shell string) []string {
	if isLegacyWSLBashPath(shell) {
		return []string{"-s"}
	}
	return []string{"-c"}
}

// isLegacyWSLBashPath reports a Windows Subsystem for Linux bash launcher that
// reads the command from stdin.
func isLegacyWSLBashPath(shell string) bool {
	normalized := strings.ToLower(filepath.ToSlash(shell))
	return runtime.GOOS == "windows" && strings.HasSuffix(normalized, "/windows/system32/bash.exe")
}

// shellEnv builds the child environment (upstream getShellEnv).
func shellEnv(base map[string]string, options *ShellExecOptions) []string {
	combined := map[string]string{}
	inherit := true
	if options != nil {
		inherit = options.InheritEnv
	}
	if inherit {
		for _, entry := range os.Environ() {
			if index := strings.IndexByte(entry, '='); index > 0 {
				combined[entry[:index]] = entry[index+1:]
			}
		}
	}
	for key, value := range base {
		combined[key] = value
	}
	if options != nil {
		for key, value := range options.Env {
			combined[key] = value
		}
	}
	env := make([]string, 0, len(combined))
	for key, value := range combined {
		env = append(env, key+"="+value)
	}
	return env
}

func prefixAll(values []string) []string {
	prefixed := make([]string, 0, len(values))
	for _, value := range values {
		prefixed = append(prefixed, "  "+value)
	}
	return prefixed
}

// utf8ChunkReader decodes complete runes from byte chunks, holding an
// incomplete trailing rune for the next chunk (upstream's streaming decoder).
type utf8ChunkReader struct {
	remainder []byte
}

func (r *utf8ChunkReader) decode(chunk []byte) string {
	data := chunk
	if len(r.remainder) > 0 {
		data = append(append([]byte{}, r.remainder...), chunk...)
		r.remainder = nil
	}
	if len(data) == 0 {
		return ""
	}
	if start := partialRuneStart(data); start >= 0 {
		r.remainder = append([]byte{}, data[start:]...)
		data = data[:start]
	}
	return string(data)
}

// partialRuneStart returns the index of an incomplete trailing UTF-8 rune, or
// -1 when the data ends on a complete rune.
func partialRuneStart(data []byte) int {
	for back := 1; back <= 3 && back <= len(data); back++ {
		value := data[len(data)-back]
		if value < 0x80 {
			return -1
		}
		if value&0xC0 == 0x80 {
			continue
		}
		size := expectedRuneLen(value)
		if size == 0 {
			return -1
		}
		if size > back {
			return len(data) - back
		}
		return -1
	}
	return -1
}

// expectedRuneLen is the UTF-8 sequence length a lead byte promises.
func expectedRuneLen(value byte) int {
	switch {
	case value&0x80 == 0:
		return 1
	case value&0xE0 == 0xC0:
		return 2
	case value&0xF0 == 0xE0:
		return 3
	case value&0xF8 == 0xF0:
		return 4
	}
	return 0
}

var _ Shell = (*OSShell)(nil)
