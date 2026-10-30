package mcp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// Stdio transport tuning (upstream stdio.ts).
var stdioStartProcess = func(command *exec.Cmd) error { return command.Start() }

const (
	defaultMaxStderrBytes = 64 * 1024
	defaultCloseTimeoutMs = 2000
	// StdinCloseGraceMs is how long a server gets to exit on its own after
	// stdin closes, before it is sent SIGTERM.
	StdinCloseGraceMs = 500
)

// StdioTransportOptions configures a StdioTransport (upstream
// StdioTransportOptions).
type StdioTransportOptions struct {
	Command string
	Args    []string
	Cwd     string
	Env     map[string]string
	// InheritEnv is nil or true to run the server with this process's
	// environment plus Env; false uses only Env.
	InheritEnv      *bool
	Stderr          string // "pipe" (default) or "inherit"
	OnStderr        func(chunk string)
	MaxMessageBytes int64
	// MaxMessageBytesSet distinguishes an explicit zero limit from defaulting.
	MaxMessageBytesSet bool
	MaxStderrBytes     int64
	// MaxStderrBytesSet distinguishes explicit zero (no truncation) from defaulting.
	MaxStderrBytesSet bool
	// CloseTimeoutMs is how long the server gets after SIGTERM before
	// SIGKILL. Default: 2000.
	CloseTimeoutMs int64
	// CloseTimeoutMsSet distinguishes an explicit zero from the default.
	CloseTimeoutMsSet bool
}

// StdioTransport runs a newline-delimited MCP server as a child process
// (upstream StdioTransport). The server runs in its own process group on
// POSIX systems, so closing the transport terminates the server's children
// too; on Windows taskkill /T /F does that instead.
type StdioTransport struct {
	transportEvents
	options StdioTransportOptions

	mu            sync.Mutex
	child         *exec.Cmd
	pid           int
	stdin         io.WriteCloser
	stdinWriteMu  sync.Mutex
	stdoutBuffer  []byte
	stderrBuffer  []byte
	started       bool
	startDone     chan struct{}
	closed        bool
	closeReported bool
	exitCh        chan struct{}
	readers       sync.WaitGroup
}

// NewStdioTransport builds a transport; nothing runs until Start.
func NewStdioTransport(options StdioTransportOptions) *StdioTransport {
	return &StdioTransport{options: options, exitCh: make(chan struct{})}
}

// PID returns the server's process id, or 0 before Start.
func (t *StdioTransport) PID() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pid
}

// Stderr returns everything the server wrote to stderr, trimmed to the last
// MaxStderrBytes.
func (t *StdioTransport) Stderr() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	// stdio.ts's getter is stderrBuffer.toString("utf8"): malformed bytes
	// become replacement characters.
	return decodeUTF8(t.stderrBuffer)
}

// SetProtocolVersion is unused on stdio (the wire has no version header).
func (t *StdioTransport) SetProtocolVersion(string) {}

// Start spawns the server.
func (t *StdioTransport) Start(ctx context.Context) error {
	t.mu.Lock()
	if t.started {
		t.mu.Unlock()
		return fmt.Errorf("MCP stdio transport already started")
	}
	if t.closed {
		t.mu.Unlock()
		return &protocol.McpConnectionClosedError{}
	}
	t.started = true
	startDone := make(chan struct{})
	t.startDone = startDone
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		if t.startDone == startDone {
			t.startDone = nil
			close(startDone)
		}
		t.mu.Unlock()
	}()

	options := t.options
	env := os.Environ()
	if options.InheritEnv != nil && !*options.InheritEnv {
		env = nil
	}
	for key, value := range options.Env {
		env = append(env, key+"="+value)
	}
	stderrPiped := options.Stderr != "inherit"

	cmd := exec.Command(options.Command, options.Args...)
	cmd.Dir = options.Cwd
	cmd.Env = env
	if !stderrPiped {
		cmd.Stderr = os.Stderr
	}
	// Own process group so closing the transport can terminate the server's
	// children too (upstream detached: USE_PROCESS_GROUPS).
	configureProcessGroup(cmd)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	// Own the output pipes: Cmd.Wait closes StdoutPipe/StderrPipe before
	// their readers necessarily finish, losing a fast server's final output.
	stdoutPipe, stdoutWriter, err := os.Pipe()
	if err != nil {
		_ = stdinPipe.Close()
		return err
	}
	defer stdoutWriter.Close()
	cmd.Stdout = stdoutWriter
	var stderrPipe *os.File
	spawned := false
	defer func() {
		if !spawned {
			_ = stdinPipe.Close()
			_ = stdoutPipe.Close()
			if stderrPipe != nil {
				_ = stderrPipe.Close()
			}
		}
	}()
	if stderrPiped {
		var stderrWriter *os.File
		stderrPipe, stderrWriter, err = os.Pipe()
		if err != nil {
			return err
		}
		defer stderrWriter.Close()
		cmd.Stderr = stderrWriter
	}
	if err := stdioStartProcess(cmd); err != nil {
		return err
	}
	spawned = true

	t.mu.Lock()
	t.child = cmd
	t.pid = cmd.Process.Pid
	t.stdin = stdinPipe
	t.mu.Unlock()

	if stderrPiped {
		t.readers.Add(1)
		go t.pumpStderr(stderrPipe)
	}
	t.readers.Add(1)
	go t.pumpStdout(stdoutPipe)
	installExitHook()
	// Register before watchExit starts: an instantly-exiting server must not
	// unregister an entry that was never added.
	registerLiveProcessGroup(t.pid)
	go t.watchExit(cmd, t.pid)
	return nil
}

func (t *StdioTransport) pumpStderr(pipe io.ReadCloser) {
	defer t.readers.Done()
	defer pipe.Close()
	buf := make([]byte, 32*1024)
	for {
		n, err := pipe.Read(buf)
		if n > 0 {
			t.handleStderr(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (t *StdioTransport) pumpStdout(pipe io.ReadCloser) {
	defer t.readers.Done()
	defer pipe.Close()
	buf := make([]byte, 64*1024)
	for {
		n, err := pipe.Read(buf)
		if n > 0 {
			t.handleStdout(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// watchExit is the single child-exit watcher: it runs Wait exactly once,
// drops the process group from the live set, and finalizes the close path.
func (t *StdioTransport) watchExit(cmd *exec.Cmd, pid int) {
	_ = cmd.Wait()
	// Publish process exit before waiting for readers, since a reader callback
	// may synchronously call Close. The close event itself follows pipe EOF.
	close(t.exitCh)
	t.readers.Wait()
	unregisterLiveProcessGroup(pid)
	t.mu.Lock()
	t.child = nil
	t.mu.Unlock()
	t.finishClose()
}

// finishClose runs once and emits close. As in upstream stdio.ts,
// bytes after the last newline are ignored because they never formed a message.
func (t *StdioTransport) finishClose() {
	t.mu.Lock()
	if t.closeReported {
		t.mu.Unlock()
		return
	}
	t.closeReported = true
	t.stdoutBuffer = nil
	t.mu.Unlock()
	t.emitClose()
}

func (t *StdioTransport) handleStdout(chunk []byte) {
	maxMessageBytes := t.options.MaxMessageBytes
	if maxMessageBytes == 0 && !t.options.MaxMessageBytesSet {
		maxMessageBytes = DefaultMaxMessageBytes
	}
	var overflow bool
	t.mu.Lock()
	t.stdoutBuffer = append(t.stdoutBuffer, chunk...)
	for {
		newline := bytes.IndexByte(t.stdoutBuffer, '\n')
		if newline < 0 {
			if int64(len(t.stdoutBuffer)) > maxMessageBytes {
				t.stdoutBuffer = t.stdoutBuffer[:0]
				overflow = true
			}
			break
		}
		line := t.stdoutBuffer[:newline]
		if int64(len(line)) > maxMessageBytes {
			t.stdoutBuffer = append(t.stdoutBuffer[:0], t.stdoutBuffer[newline+1:]...)
			t.mu.Unlock()
			t.emitError(fmt.Errorf("MCP stdio message exceeds %d bytes", maxMessageBytes))
			t.mu.Lock()
			continue
		}
		// stdio.ts decodes with Buffer.toString("utf8"), replacing malformed
		// bytes; Go keeps them raw unless converted.
		text := strings.TrimSuffix(decodeUTF8(line), "\r")
		t.stdoutBuffer = append(t.stdoutBuffer[:0], t.stdoutBuffer[newline+1:]...)
		t.mu.Unlock()
		t.emitLine(text)
		t.mu.Lock()
	}
	t.mu.Unlock()
	if overflow {
		t.emitError(fmt.Errorf("MCP stdio message exceeds %d bytes", maxMessageBytes))
	}
}

func (t *StdioTransport) emitLine(text string) {
	if jsTrim(text) == "" {
		return
	}
	message, err := protocol.DecodeMessage([]byte(text))
	if err != nil {
		t.emitError(err)
		return
	}
	t.emitMessage(Message(message))
}

func (t *StdioTransport) handleStderr(chunk []byte) {
	maxStderrBytes := t.options.MaxStderrBytes
	if maxStderrBytes == 0 && !t.options.MaxStderrBytesSet {
		maxStderrBytes = defaultMaxStderrBytes
	}
	t.mu.Lock()
	t.stderrBuffer = append(t.stderrBuffer, chunk...)
	if !(maxStderrBytes == 0 && t.options.MaxStderrBytesSet) && int64(len(t.stderrBuffer)) > maxStderrBytes {
		start := int64(len(t.stderrBuffer)) - maxStderrBytes
		if maxStderrBytes < 0 {
			if maxStderrBytes < -int64(len(t.stderrBuffer)) {
				start = int64(len(t.stderrBuffer))
			} else {
				start = -maxStderrBytes
			}
		}
		t.stderrBuffer = t.stderrBuffer[int(start):]
	}
	t.mu.Unlock()
	if t.options.OnStderr != nil {
		t.options.OnStderr(decodeUTF8(chunk))
	}
}

// Send writes one newline-terminated JSON-RPC message to the server's
// stdin.
func (t *StdioTransport) Send(ctx context.Context, message Message) error {
	t.mu.Lock()
	started, closed, stdin := t.started, t.closed, t.stdin
	t.mu.Unlock()
	if !started || closed || stdin == nil {
		return &protocol.McpConnectionClosedError{}
	}
	payload, err := protocol.MarshalJSON(message)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	t.stdinWriteMu.Lock()
	defer t.stdinWriteMu.Unlock()
	_, err = stdin.Write(payload)
	return err
}

// Close shuts the server down per the spec: close stdin and let the server
// exit, then SIGTERM, then SIGKILL.
func (t *StdioTransport) Close(ctx context.Context) error {
	var child *exec.Cmd
	var pid int
	for {
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			return nil
		}
		if startDone := t.startDone; startDone != nil {
			t.mu.Unlock()
			<-startDone
			continue
		}
		t.closed = true
		child = t.child
		pid = t.pid
		t.mu.Unlock()
		break
	}
	if child == nil {
		t.finishClose()
		return nil
	}
	select {
	case <-t.exitCh:
		// Already exited; the exit watcher finalized the close path.
		_ = t.closeStdin()
		return nil
	default:
	}
	closeTimeoutMs := t.options.CloseTimeoutMs
	if closeTimeoutMs == 0 && !t.options.CloseTimeoutMsSet {
		closeTimeoutMs = defaultCloseTimeoutMs
	}
	grace := int64(StdinCloseGraceMs)
	if closeTimeoutMs < grace {
		grace = closeTimeoutMs
	}
	graceTimer := time.NewTimer(javascriptTimerDuration(grace))
	defer graceTimer.Stop()
	killTimer := time.NewTimer(javascriptTimerDuration(grace + closeTimeoutMs))
	defer killTimer.Stop()
	_ = t.closeStdin()
	select {
	case <-t.exitCh:
	case <-killTimer.C:
		killProcessTree(pid, syscall.SIGKILL)
		<-t.exitCh
	case <-graceTimer.C:
		killProcessTree(pid, syscall.SIGTERM)
		select {
		case <-t.exitCh:
		case <-killTimer.C:
			killProcessTree(pid, syscall.SIGKILL)
			<-t.exitCh
		}
	}
	// Children of the server that ignored stdin closing would otherwise
	// outlive it.
	killProcessTree(pid, syscall.SIGTERM)
	unregisterLiveProcessGroup(pid)
	t.mu.Lock()
	t.child = nil
	t.mu.Unlock()
	// The watcher emits close after the output readers finish. Waiting for
	// them here would deadlock a listener that called Close synchronously.
	return nil
}

func (t *StdioTransport) closeStdin() error {
	t.mu.Lock()
	stdin := t.stdin
	t.stdin = nil
	t.mu.Unlock()
	if stdin == nil {
		return nil
	}
	return stdin.Close()
}

// killProcessTree signals the whole process group (upstream
// killProcessTree); the group kill keeps wrappers like npx or uvx from
// leaving the server behind, and the direct kill is the fallback for a
// process that was never made a group leader.
func killProcessTree(pid int, sig syscall.Signal) {
	if pid <= 0 {
		return
	}
	if err := killProcessGroup(pid, sig); err == nil {
		return
	}
	_ = killProcess(pid, sig)
}

// Live process groups, killed if the host exits without closing them
// (upstream liveProcessGroups + installExitHook). Upstream runs the hook on
// process exit; a Go library cannot hook os.Exit, so the hook watches the
// terminating signals, kills the groups, and re-raises (D182).
var (
	liveProcessGroupsMu sync.Mutex
	liveProcessGroups   = map[int]bool{}
	exitHookInstalled   bool
	exitHookMu          sync.Mutex
)

func registerLiveProcessGroup(pid int) {
	liveProcessGroupsMu.Lock()
	liveProcessGroups[pid] = true
	liveProcessGroupsMu.Unlock()
}

func unregisterLiveProcessGroup(pid int) {
	liveProcessGroupsMu.Lock()
	delete(liveProcessGroups, pid)
	liveProcessGroupsMu.Unlock()
}

func installExitHook() {
	exitHookMu.Lock()
	defer exitHookMu.Unlock()
	if exitHookInstalled {
		return
	}
	exitHookInstalled = true
	channel := make(chan os.Signal, 1)
	signal.Notify(channel, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		received := <-channel
		killLiveProcessGroups()
		// Restore the default action and re-raise, so the host still dies
		// the way it would have.
		if sig, ok := received.(syscall.Signal); ok {
			signal.Reset(sig)
			_ = raiseSelf(sig)
		}
	}()
}

func killLiveProcessGroups() {
	liveProcessGroupsMu.Lock()
	defer liveProcessGroupsMu.Unlock()
	for pid := range liveProcessGroups {
		_ = killProcessGroup(pid, syscall.SIGTERM)
	}
}
