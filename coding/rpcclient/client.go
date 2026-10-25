package rpcclient

// Port of pi v1.4.0 modes/rpc/rpc-client.ts: a typed client for the stdio RPC
// mode. It spawns the CLI with `--mode rpc` and speaks the JSONL protocol over
// the child's stdin/stdout. Only the extension-free command surface exists
// (D41), so the command methods cover exactly the commands rpc-mode.ts
// implements.
//
// JSON key order is insertion order upstream. The server parses by key, so the
// requests here marshal Go maps; ordering does not affect the protocol.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// DefaultRequestTimeout bounds one command round trip (upstream 30s).
const DefaultRequestTimeout = 30 * time.Second

// DefaultIdleTimeout bounds WaitForIdle/CollectEvents (upstream 60s).
const DefaultIdleTimeout = 60 * time.Second

// Options configure an RpcClient.
type Options struct {
	// CLIPath is the executable to spawn. It defaults to the pier binary name
	// resolved on PATH (upstream defaults to dist/cli.js).
	CLIPath string
	// Cwd is the child's working directory.
	Cwd string
	// Env adds or overrides environment variables for the child.
	Env map[string]string
	// Provider and Model are convenience flags (--provider/--model).
	Provider string
	Model    string
	// Args are extra CLI arguments appended after the derived ones.
	Args []string
	// PeerArgs, when non-empty, replaces the derived argv entirely. It embeds
	// a custom RPC peer instead of the pier CLI (tests and alternate hosts).
	PeerArgs []string
	// RequestTimeout overrides DefaultRequestTimeout.
	RequestTimeout time.Duration
}

// Event is one streamed session event (upstream JsonAgentSessionEvent). Go has
// no discriminated union, so the event is its decoded JSON object; Type reads
// the "type" discriminator.
type Event map[string]any

// Type returns the event's "type" discriminator.
func (e Event) Type() string {
	name, _ := e["type"].(string)
	return name
}

// EventListener receives every non-response record.
type EventListener func(Event)

// ModelInfo is the subset of a model the client surfaces (upstream ModelInfo).
type ModelInfo struct {
	Provider      string `json:"provider"`
	ID            string `json:"id"`
	ContextWindow int    `json:"contextWindow"`
	Reasoning     bool   `json:"reasoning"`
}

// SessionState is the get_state response (upstream RpcSessionState).
type SessionState struct {
	Model                 *ai.Model `json:"model"`
	ThinkingLevel         string    `json:"thinkingLevel"`
	IsStreaming           bool      `json:"isStreaming"`
	IsCompacting          bool      `json:"isCompacting"`
	SteeringMode          string    `json:"steeringMode"`
	FollowUpMode          string    `json:"followUpMode"`
	SessionFile           string    `json:"sessionFile"`
	SessionID             string    `json:"sessionId"`
	SessionName           string    `json:"sessionName"`
	AutoCompactionEnabled bool      `json:"autoCompactionEnabled"`
	MessageCount          int       `json:"messageCount"`
	PendingMessageCount   int       `json:"pendingMessageCount"`
}

// ClearQueueResult is the clear_queue payload.
type ClearQueueResult struct {
	Steering []string `json:"steering"`
	FollowUp []string `json:"followUp"`
}

// SessionResult is the new_session/switch_session/clone payload.
type SessionResult struct {
	Cancelled bool `json:"cancelled"`
}

// ForkResult is the fork payload; Text is omitted for an "at" fork.
type ForkResult struct {
	Text      string `json:"text"`
	Cancelled bool   `json:"cancelled"`
}

// ForkMessage is one get_fork_messages entry.
type ForkMessage struct {
	EntryID string `json:"entryId"`
	Text    string `json:"text"`
}

// CycleModelResult is the non-null cycle_model payload.
type CycleModelResult struct {
	Model         *ai.Model `json:"model"`
	ThinkingLevel string    `json:"thinkingLevel"`
	IsScoped      bool      `json:"isScoped"`
}

// ExportResult is the export_html payload.
type ExportResult struct {
	Path string `json:"path"`
}

// EntriesResult is the get_entries payload.
type EntriesResult struct {
	Entries []coding.SessionEntry `json:"entries"`
	LeafID  *string               `json:"leafId"`
}

// TreeResult is the get_tree payload.
type TreeResult struct {
	Tree   []coding.SessionTreeNode `json:"tree"`
	LeafID *string                  `json:"leafId"`
}

// SourceInfo is a resource's origin (upstream SourceInfo).
type SourceInfo struct {
	Path    string `json:"path"`
	Source  string `json:"source"`
	Scope   string `json:"scope"`
	Origin  string `json:"origin"`
	BaseDir string `json:"baseDir,omitempty"`
}

// SlashCommand is one get_commands entry.
type SlashCommand struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Source      string     `json:"source"`
	SourceInfo  SourceInfo `json:"sourceInfo"`
}

// response is one correlated response record.
type response struct {
	Type    string          `json:"type"`
	Command string          `json:"command"`
	Success bool            `json:"success"`
	Error   string          `json:"error"`
	Data    json.RawMessage `json:"data"`
}

func (r *response) decode(target any) error {
	if !r.Success {
		return errors.New(r.Error)
	}
	if len(r.Data) == 0 || string(r.Data) == "null" {
		return nil
	}
	return json.Unmarshal(r.Data, target)
}

// pendingRequest is one in-flight command.
type pendingRequest struct {
	done     chan struct{}
	response *response
	err      error
}

// RpcClient is a started-or-stopped subprocess client.
type RpcClient struct {
	options Options

	mu          sync.Mutex
	process     *exec.Cmd
	stdin       io.WriteCloser
	started     bool
	exitErr     error
	processDone chan struct{}
	pending     map[string]*pendingRequest
	nextID      int
	listeners   []eventListener
	listenerSeq int
	stderr      strings.Builder

	writeMu sync.Mutex

	// sendFunc replaces the process transport in tests, like upstream's
	// overridable send.
	sendFunc func(command map[string]any) (*response, error)
}

// eventListener carries the unsubscribe identity for one subscription.
type eventListener struct {
	id       int
	listener EventListener
}

// New returns a client for the given options.
func New(options Options) *RpcClient {
	return &RpcClient{options: options, pending: map[string]*pendingRequest{}}
}

// Stderr returns the child's collected stderr output.
func (c *RpcClient) Stderr() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stderr.String()
}

// OnEvent subscribes to streamed events and returns an unsubscribe function.
func (c *RpcClient) OnEvent(listener EventListener) func() {
	c.mu.Lock()
	id := c.listenerSeq
	c.listenerSeq++
	c.listeners = append(c.listeners, eventListener{id: id, listener: listener})
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			for index, entry := range c.listeners {
				if entry.id == id {
					c.listeners = append(c.listeners[:index], c.listeners[index+1:]...)
					break
				}
			}
			c.mu.Unlock()
		})
	}
}

// Start spawns the child process and begins reading its stdout. Canceling ctx
// terminates the child.
func (c *RpcClient) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return errors.New("Client already started")
	}
	c.started = true // Reserve admission before spawning outside the data mutex.
	c.mu.Unlock()
	spawned := false
	defer func() {
		if !spawned {
			c.mu.Lock()
			c.started = false
			c.mu.Unlock()
		}
	}()

	cliPath := c.options.CLIPath
	if cliPath == "" {
		cliPath = coding.AppName
	}
	var args []string
	if len(c.options.PeerArgs) > 0 {
		args = append(args, c.options.PeerArgs...)
	} else {
		args = []string{"--mode", "rpc"}
		if c.options.Provider != "" {
			args = append(args, "--provider", c.options.Provider)
		}
		if c.options.Model != "" {
			args = append(args, "--model", c.options.Model)
		}
		args = append(args, c.options.Args...)
	}

	command := exec.CommandContext(ctx, cliPath, args...)
	command.Dir = c.options.Cwd
	command.Env = os.Environ()
	for key, value := range c.options.Env {
		command.Env = append(command.Env, key+"="+value)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return err
	}
	// Own the output pipes: Cmd.Wait closes StdoutPipe/StderrPipe even when
	// an event callback has not returned to drain the remaining peer output.
	stdout, stdoutWriter, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		return err
	}
	defer stdoutWriter.Close()
	stderr, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return err
	}
	defer stderrWriter.Close()
	command.Stdout, command.Stderr = stdoutWriter, stderrWriter
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return err
	}
	spawned = true
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()

	c.mu.Lock()
	c.process, c.stdin = command, stdin
	c.exitErr = nil
	c.processDone = make(chan struct{})
	done := c.processDone
	c.mu.Unlock()

	go func() {
		defer stderr.Close()
		c.drainStderr(stderr)
	}()
	go func() {
		defer stdout.Close()
		c.readLines(stdout)
	}()
	go func() {
		waitErr := command.Wait()
		exitErrValue := c.processExitError(waitErr)
		c.mu.Lock()
		if c.exitErr == nil {
			c.exitErr = exitErrValue
		}
		exitErr := c.exitErr
		c.mu.Unlock()
		c.rejectPending(exitErr)
		close(done)
	}()

	// Upstream waits briefly for initialization, then reports a process that
	// already exited.
	timer := time.NewTimer(100 * time.Millisecond)
	select {
	case <-timer.C:
	case <-ctx.Done():
		timer.Stop()
	}
	c.mu.Lock()
	exitErr := c.exitErr
	c.mu.Unlock()
	if exitErr != nil {
		return exitErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// Stop terminates the child, escalating to a kill after one second.
func (c *RpcClient) Stop() {
	c.mu.Lock()
	command := c.process
	done := c.processDone
	if command == nil || command.Process == nil {
		c.mu.Unlock()
		return
	}
	c.process = nil
	c.stdin = nil
	process := command.Process
	c.mu.Unlock()

	if runtime.GOOS == "windows" {
		_ = process.Kill()
	} else {
		_ = process.Signal(syscall.SIGTERM)
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(time.Second):
			_ = process.Kill()
			<-done
		}
	}
	// A listener can call Stop from the stdout reader. Join only process exit,
	// not the reader callbacks, which would make that listener wait on itself.
	c.mu.Lock()
	c.pending = map[string]*pendingRequest{}
	c.started = false
	c.mu.Unlock()
}

func (c *RpcClient) drainStderr(stderr io.Reader) {
	reader := bufio.NewReader(stderr)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			c.mu.Lock()
			c.stderr.WriteString(line)
			c.mu.Unlock()
			_, _ = os.Stderr.WriteString(line)
		}
		if err != nil {
			return
		}
	}
}

func (c *RpcClient) readLines(stdout io.Reader) {
	reader := bufio.NewReader(stdout)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) != 0 {
			c.handleLine(strings.TrimSuffix(string(line), "\n"))
		}
		if err != nil {
			return
		}
	}
}

func (c *RpcClient) handleLine(line string) {
	// Upstream catches the entire dispatch, including listener failures, so a
	// failed callback does not terminate stdout reading or pending requests.
	defer func() { _ = recover() }()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(line), &decoded); err != nil {
		return // Ignore non-JSON lines.
	}
	if decoded["type"] == "response" {
		id, _ := decoded["id"].(string)
		if id != "" && c.resolve(id, line) {
			return
		}
	}
	c.mu.Lock()
	listeners := make([]EventListener, 0, len(c.listeners))
	for _, entry := range c.listeners {
		listeners = append(listeners, entry.listener)
	}
	c.mu.Unlock()
	event := Event(decoded)
	for _, listener := range listeners {
		listener(event)
	}
}

func (c *RpcClient) resolve(id, line string) bool {
	c.mu.Lock()
	pending := c.pending[id]
	if pending == nil {
		c.mu.Unlock()
		return false
	}
	delete(c.pending, id)
	c.mu.Unlock()
	var parsed response
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		pending.err = err
	} else {
		pending.response = &parsed
	}
	close(pending.done)
	return true
}

func (c *RpcClient) rejectPending(err error) {
	c.mu.Lock()
	pending := c.pending
	c.pending = map[string]*pendingRequest{}
	c.mu.Unlock()
	for _, request := range pending {
		request.err = err
		close(request.done)
	}
}

func (c *RpcClient) send(command map[string]any) (*response, error) {
	if c.sendFunc != nil {
		return c.sendFunc(command)
	}
	c.mu.Lock()
	commandName, _ := command["type"].(string)
	if !c.started {
		c.mu.Unlock()
		return nil, errors.New("Client not started")
	}
	stdin := c.stdin
	if stdin == nil {
		c.mu.Unlock()
		return nil, errors.New("Client not started")
	}
	if c.exitErr != nil {
		exitErr := c.exitErr
		c.mu.Unlock()
		return nil, exitErr
	}
	timeout := c.options.RequestTimeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	c.nextID++
	id := "req_" + strconv.Itoa(c.nextID)
	request := &pendingRequest{done: make(chan struct{})}
	c.pending[id] = request
	c.mu.Unlock()

	full := make(map[string]any, len(command)+1)
	for key, value := range command {
		full[key] = value
	}
	full["id"] = id
	encoded, err := ai.MarshalJSON(full)
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	c.writeMu.Lock()
	_, err = stdin.Write(append(encoded, '\n'))
	c.writeMu.Unlock()
	if err != nil {
		writeErr := fmt.Errorf("Agent process stdin error: %v. Stderr: %s", err, c.Stderr())
		c.mu.Lock()
		delete(c.pending, id)
		c.exitErr = writeErr
		c.mu.Unlock()
		return nil, writeErr
	}

	select {
	case <-request.done:
		if request.err != nil {
			return nil, request.err
		}
		return request.response, nil
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("Timeout waiting for response to %s. Stderr: %s", commandName, c.Stderr())
	}
}

func (c *RpcClient) processExitError(waitErr error) error {
	code := "null"
	signal := "null"
	if waitErr == nil {
		code = "0"
	}
	var exit *exec.ExitError
	if errors.As(waitErr, &exit) {
		if exit.ExitCode() >= 0 {
			code = strconv.Itoa(exit.ExitCode())
		}
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			signal = status.Signal().String()
		}
	}
	return fmt.Errorf("Agent process exited (code=%s signal=%s). Stderr: %s", code, signal, c.Stderr())
}

// WaitForIdle resolves on the next agent_settled event.
func (c *RpcClient) WaitForIdle(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultIdleTimeout
	}
	done := make(chan struct{})
	var once sync.Once
	unsubscribe := c.OnEvent(func(event Event) {
		if event.Type() == "agent_settled" {
			once.Do(func() { close(done) })
		}
	})
	defer unsubscribe()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("Timeout waiting for agent to become idle. Stderr: %s", c.Stderr())
	}
}

// eventCollector gathers events through agent_settled. Subscription happens
// in startEventCollector, so a caller that prompts afterwards cannot miss an
// event emitted between the prompt and a late subscription (upstream's
// Promise executor subscribes synchronously, before the prompt is sent).
type eventCollector struct {
	mu     sync.Mutex
	events []Event
	done   chan struct{}
	once   sync.Once
}

func (c *RpcClient) startEventCollector() (*eventCollector, func()) {
	collector := &eventCollector{done: make(chan struct{})}
	unsubscribe := c.OnEvent(func(event Event) {
		collector.mu.Lock()
		collector.events = append(collector.events, event)
		collector.mu.Unlock()
		if event.Type() == "agent_settled" {
			collector.once.Do(func() { close(collector.done) })
		}
	})
	return collector, unsubscribe
}

func (c *RpcClient) collectUntil(collector *eventCollector, timeout <-chan time.Time) ([]Event, error) {
	select {
	case <-collector.done:
		collector.mu.Lock()
		defer collector.mu.Unlock()
		return append([]Event(nil), collector.events...), nil
	case <-timeout:
		return nil, fmt.Errorf("Timeout collecting events. Stderr: %s", c.Stderr())
	}
}

// CollectEvents collects events until agent_settled.
func (c *RpcClient) CollectEvents(timeout time.Duration) ([]Event, error) {
	if timeout <= 0 {
		timeout = DefaultIdleTimeout
	}
	collector, unsubscribe := c.startEventCollector()
	defer unsubscribe()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	return c.collectUntil(collector, timer.C)
}

// PromptAndWait sends a prompt and returns the events through agent_settled.
func (c *RpcClient) PromptAndWait(message string, images []ai.ImageContent, timeout time.Duration) ([]Event, error) {
	if timeout <= 0 {
		timeout = DefaultIdleTimeout
	}
	collector, unsubscribe := c.startEventCollector()
	defer unsubscribe()
	// Subscribe before sending, like upstream's collectEvents promise, and
	// start the clock there too.
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	if _, err := c.Prompt(message, images, ""); err != nil {
		return nil, err
	}
	return c.collectUntil(collector, timer.C)
}
