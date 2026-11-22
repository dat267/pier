package mcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/dat267/pier/internal/leakcheck"
	"github.com/dat267/pier/mcp/protocol"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Port of packages/mcp/test/stdio.test.ts against Go ports of the upstream
// fixtures (mcp/testdata/stdiofixture, stubbornfixture), built by TestMain.

var (
	stdioFixtureBin    string
	stubbornFixtureBin string
)

func TestMain(m *testing.M) {
	if helper := os.Getenv("PIER_MCP_UTF8_HELPER"); helper != "" {
		if helper == "partial" {
			_, _ = os.Stdout.Write([]byte(`{"jsonrpc":"2.0"}`))
			return
		}
		_, _ = os.Stdout.Write([]byte("{\"jsonrpc\":\"2.0\",\"method\":\"\xff\xff\"}\n"))
		_, _ = os.Stderr.Write([]byte("\ufeff\xff\xff"))
		if helper == "wait" {
			_, _ = io.Copy(io.Discard, os.Stdin)
		}
		return
	}
	tempDir, err := os.MkdirTemp("", "pier-mcp-fixtures")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, fixture := range []struct {
		pkg, out string
	}{
		{"github.com/dat267/pier/mcp/testdata/stdiofixture", "stdiofixture"},
		{"github.com/dat267/pier/mcp/testdata/stubbornfixture", "stubbornfixture"},
	} {
		bin := filepath.Join(tempDir, fixture.out)
		build := exec.Command("go", "build", "-o", bin, fixture.pkg)
		build.Dir = moduleRoot()
		if out, err := build.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "building %s failed: %s\n%s", fixture.pkg, err, out)
			os.Exit(1)
		}
		if fixture.out == "stdiofixture" {
			stdioFixtureBin = bin
		} else {
			stubbornFixtureBin = bin
		}
	}
	code := leakcheck.Run(m)
	_ = os.RemoveAll(tempDir)
	os.Exit(code)
}

// moduleRoot finds the directory containing go.mod, so the test works from
// any package directory.
func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

func TestStdioTransportCloseWaitsForStart(t *testing.T) {
	originalStart := stdioStartProcess
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseStart := func() { releaseOnce.Do(func() { close(release) }) }
	stdioStartProcess = func(command *exec.Cmd) error {
		close(entered)
		<-release
		return command.Start()
	}
	defer func() {
		releaseStart()
		stdioStartProcess = originalStart
	}()

	transport := NewStdioTransport(StdioTransportOptions{Command: stdioFixtureBin})
	startDone := make(chan error, 1)
	go func() { startDone <- transport.Start(context.Background()) }()
	<-entered
	closeDone := make(chan error, 1)
	go func() { closeDone <- transport.Close(context.Background()) }()

	var earlyClose bool
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
		earlyClose = true
	case <-time.After(25 * time.Millisecond):
	}
	releaseStart()
	if err := <-startDone; err != nil {
		t.Fatal(err)
	}
	if !earlyClose {
		select {
		case err := <-closeDone:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("Close did not finish after startup")
		}
	}
	pid := transport.PID()
	if pid != 0 {
		process, _ := os.FindProcess(pid)
		if process != nil {
			_ = process.Kill()
		}
		select {
		case <-transport.exitCh:
		case <-time.After(2 * time.Second):
			t.Fatal("cleanup process did not exit")
		}
	}
	if earlyClose {
		t.Fatal("Close returned before the in-progress child startup was handled")
	}
}

// stdio.ts uses Buffer.subarray(-maxStderrBytes); a negative limit must not panic.
func TestStdioTransportNegativeStderrLimitDoesNotPanic(t *testing.T) {
	transport := NewStdioTransport(StdioTransportOptions{MaxStderrBytes: -1})
	transport.handleStderr([]byte("abcd"))
	if got := transport.Stderr(); got != "bcd" {
		t.Fatalf("stderr = %q; want Buffer.subarray(1) result", got)
	}
}

func TestStdioTransportWaitsForProcessExitAfterStdoutEOF(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
	transport := NewStdioTransport(StdioTransportOptions{
		Command: "/bin/sh",
		Args:    []string{"-c", "exec 1>&-; sleep 0.4"},
	})
	closed := make(chan struct{}, 1)
	transport.OnClose(func() { closed <- struct{}{} })
	if err := transport.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
		t.Fatal("transport closed on stdout EOF before the process exited")
	case <-time.After(100 * time.Millisecond):
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("transport did not close when the process exited")
	}
}

func TestStdioTransportConnectsAndCapturesStderr(t *testing.T) {
	var stderrMu sync.Mutex
	stderrChunks := 0
	transport := NewStdioTransport(StdioTransportOptions{
		Command: stdioFixtureBin,
		OnStderr: func(chunk string) {
			stderrMu.Lock()
			stderrChunks++
			stderrMu.Unlock()
		},
	})
	client := NewClient(ClientOptions{Name: "stdio-test", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), transport); err != nil {
		t.Fatal(err)
	}
	tools, err := client.ListTools(context.Background(), RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" || string(tools[0].InputSchema) != `{"type":"object"}` {
		t.Fatalf("tools = %+v", tools)
	}
	result, err := client.CallTool(context.Background(), "echo", map[string]any{"text": "hello"}, RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "hello" {
		t.Fatalf("result = %+v", result.Content)
	}
	if transport.PID() <= 0 {
		t.Fatalf("pid = %d", transport.PID())
	}
	time.Sleep(20 * time.Millisecond)
	if transport.Stderr() == "" || !strings.Contains(transport.Stderr(), "stdio fixture ready") {
		t.Fatalf("stderr = %q", transport.Stderr())
	}
	stderrMu.Lock()
	chunks := stderrChunks
	stderrMu.Unlock()
	if chunks == 0 {
		t.Fatal("onStderr never fired")
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.ConnectionState() != StateClosed {
		t.Fatalf("state = %s", client.ConnectionState())
	}
}

func TestStdioTransportKillsStubbornServerIncludingChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stubborn fixture relies on POSIX signals")
	}
	transport := NewStdioTransport(StdioTransportOptions{
		Command:        stubbornFixtureBin,
		CloseTimeoutMs: 100,
	})
	client := NewClient(ClientOptions{Name: "stdio-test", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), transport); err != nil {
		t.Fatal(err)
	}
	var grandchild int
	deadline := time.Now().Add(10 * time.Second)
	for grandchild == 0 {
		fmt.Sscanf(transport.Stderr(), "grandchild %d", &grandchild)
		if time.Now().After(deadline) {
			t.Fatalf("grandchild pid never appeared; stderr = %q", transport.Stderr())
		}
		time.Sleep(10 * time.Millisecond)
	}

	startedAt := time.Now()
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(startedAt); elapsed > 5*time.Second {
		t.Fatalf("close took %s", elapsed)
	}
	// The grandchild must be gone: the group kill reached it.
	for range 100 {
		alive, err := processAlive(grandchild)
		if err != nil {
			t.Fatalf("probe err = %v", err)
		}
		if !alive {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("grandchild %d still alive", grandchild)
}

// TestStdioTransportRegistersLiveProcessGroup pins the exit hook's live set:
// without the registration the hook has nothing to kill, so an MCP server's
// process group outlives the host on SIGTERM (upstream stdio.ts
// liveProcessGroups.add after installExitHook).
func TestStdioTransportRegistersLiveProcessGroup(t *testing.T) {
	transport := NewStdioTransport(StdioTransportOptions{Command: stdioFixtureBin})
	client := NewClient(ClientOptions{Name: "stdio-test", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), transport); err != nil {
		t.Fatal(err)
	}
	pid := transport.PID()
	if !liveProcessGroupRegistered(pid) {
		t.Fatalf("pid %d was not registered with the exit hook", pid)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if liveProcessGroupRegistered(pid) {
		t.Fatalf("pid %d stayed registered after close", pid)
	}
}

func liveProcessGroupRegistered(pid int) bool {
	liveProcessGroupsMu.Lock()
	defer liveProcessGroupsMu.Unlock()
	return liveProcessGroups[pid]
}

// stdio.ts decodes stdout lines and stderr chunks with Buffer.toString("utf8"),
// which replaces malformed UTF-8 instead of failing the JSON parse.
func TestStdioTransportReplacesMalformedUTF8(t *testing.T) {
	transport := NewStdioTransport(StdioTransportOptions{})
	var messages []Message
	transport.OnMessage(func(message Message) { messages = append(messages, message) })
	transport.handleStdout([]byte("{\"jsonrpc\":\"2.0\",\"method\":\"m\",\"params\":{\"x\":\"\xff\"}}\n"))
	if len(messages) != 1 {
		t.Fatalf("messages = %d; want the malformed byte replaced, not rejected", len(messages))
	}
	notification, ok := messages[0].(*protocol.JsonRpcNotification)
	if !ok {
		t.Fatalf("message = %T", messages[0])
	}
	if got, want := string(notification.Params), `{"x":"`+"\uFFFD"+`"}`; got != want {
		t.Fatalf("params = %q; want %q", got, want)
	}
}

func TestStdioTransportStderrChunkReplacesMalformedUTF8(t *testing.T) {
	var seen []string
	transport := NewStdioTransport(StdioTransportOptions{OnStderr: func(chunk string) { seen = append(seen, chunk) }})
	transport.handleStderr([]byte("bad \xff byte"))
	if len(seen) != 1 || seen[0] != "bad \uFFFD byte" {
		t.Fatalf("stderr chunks = %q; want the replacement character", seen)
	}
}

// stdio.ts's stderr getter decodes with Buffer.toString("utf8").
func TestStdioTransportStderrStringReplacesMalformedUTF8(t *testing.T) {
	transport := NewStdioTransport(StdioTransportOptions{})
	transport.handleStderr([]byte("bad \xff byte"))
	if got := transport.Stderr(); got != "bad \uFFFD byte" {
		t.Fatalf("stderr = %q; want the replacement character", got)
	}
}

// pi v1.4.0 stdio.ts uses ?? for maxStderrBytes; subarray(-0) retains the full
// buffer rather than applying the default 64 KiB cap.
func TestStdioTransportExplicitZeroStderrLimitRetainsAllBytes(t *testing.T) {
	transport := NewStdioTransport(StdioTransportOptions{MaxStderrBytes: 0, MaxStderrBytesSet: true})
	chunk := strings.Repeat("x", defaultMaxStderrBytes+1)
	transport.handleStderr([]byte(chunk))
	if got := len(transport.Stderr()); got != len(chunk) {
		t.Fatalf("stderr bytes = %d, want %d with explicit zero limit", got, len(chunk))
	}
}

// pi v1.4.0 stdio.ts uses ?? for maxMessageBytes, so an explicit zero rejects
// every nonempty frame instead of selecting the default limit.
func TestStdioTransportExplicitZeroMessageLimit(t *testing.T) {
	transport := NewStdioTransport(StdioTransportOptions{MaxMessageBytes: 0, MaxMessageBytesSet: true})
	var messages []Message
	var errors []error
	transport.OnMessage(func(message Message) { messages = append(messages, message) })
	transport.OnError(func(err error) { errors = append(errors, err) })

	transport.handleStdout([]byte("{\"jsonrpc\":\"2.0\",\"method\":\"ping\"}\n"))
	if len(messages) != 0 {
		t.Fatalf("decoded %d messages despite a zero-byte limit", len(messages))
	}
	if len(errors) != 1 || !strings.Contains(errors[0].Error(), "exceeds 0 bytes") {
		t.Fatalf("errors = %v, want one zero-limit error", errors)
	}
}

// pi v1.4.0 (4a080252), packages/mcp/src/transports/stdio.ts send checks
// this.child?.stdin and throws McpConnectionClosedError after the child's close
// handler clears this.child, even if close() was never explicitly requested.
func TestStdioSendAfterServerExitReportsConnectionClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	transport := NewStdioTransport(StdioTransportOptions{
		Command: os.Args[0], Env: map[string]string{"PIER_MCP_UTF8_HELPER": "exit"},
	})
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	closed := make(chan struct{})
	transport.OnClose(func() { close(closed) })
	if err := transport.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("server did not exit:", ctx.Err())
	}

	message := &protocol.JsonRpcNotification{JSONRPC: "2.0", Method: "notifications/initialized"}
	var connectionClosed *protocol.McpConnectionClosedError
	if err := transport.Send(ctx, message); !errors.As(err, &connectionClosed) {
		t.Errorf("Send after server exit = %T: %v, want McpConnectionClosedError", err, err)
	}
	// Explicit shutdown is the matching control and must report the same error.
	if err := transport.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := transport.Send(ctx, message); !errors.As(err, &connectionClosed) {
		t.Fatalf("Send after explicit close = %T: %v, want McpConnectionClosedError", err, err)
	}
}

// pi v1.4.0 stdio.ts uses ?? for closeTimeoutMs, so zero is explicit and its
// setTimeout delays clamp to 1ms rather than selecting the 2s default.
func TestStdioTransportExplicitZeroCloseTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stubborn fixture relies on POSIX signals")
	}
	transport := NewStdioTransport(StdioTransportOptions{Command: stubbornFixtureBin, CloseTimeoutMs: 0, CloseTimeoutMsSet: true})
	if err := transport.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	pid := transport.PID()
	var grandchild int
	deadline := time.Now().Add(5 * time.Second)
	for grandchild == 0 {
		fmt.Sscanf(transport.Stderr(), "grandchild %d", &grandchild)
		if time.Now().After(deadline) {
			t.Fatalf("grandchild pid never appeared; stderr = %q", transport.Stderr())
		}
		time.Sleep(10 * time.Millisecond)
	}

	closed := make(chan error, 1)
	go func() { closed <- transport.Close(context.Background()) }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(300 * time.Millisecond):
		killProcessTree(pid, syscall.SIGKILL)
		select {
		case <-closed:
		case <-time.After(2 * time.Second):
			t.Fatal("transport close remained blocked after cleanup kill")
		}
		t.Fatal("explicit zero close timeout selected the default delay")
	}
	for range 100 {
		alive, err := processAlive(grandchild)
		if err != nil {
			t.Fatalf("probe grandchild: %v", err)
		}
		if !alive {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("grandchild %d survived zero-timeout close", grandchild)
}

// pi v1.4.0 transports/stdio.ts decodes each stdout line/stderr chunk and the
// stderr snapshot with Buffer.toString("utf8"). Distinct bad bytes are distinct
// replacement characters; unlike TextDecoder, Buffer preserves a leading BOM.
func TestStdioUTF8DecodingMatchesBuffer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	chunks := make(chan string, 8)
	transport := NewStdioTransport(StdioTransportOptions{
		Command: os.Args[0], Env: map[string]string{"PIER_MCP_UTF8_HELPER": "wait"},
		OnStderr: func(chunk string) { chunks <- chunk },
	})
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	closed := make(chan struct{})
	transport.OnClose(func() { close(closed) })
	messages := make(chan string, 1)
	transport.OnMessage(func(message Message) { messages <- message.(*protocol.JsonRpcNotification).Method })
	if err := transport.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case method := <-messages:
		if method != "\ufffd\ufffd" {
			t.Errorf("notification method=%q, want two replacement characters", method)
		}
	case <-ctx.Done():
		t.Fatal("stdout notification was not decoded:", ctx.Err())
	}
	var reported strings.Builder
	select {
	case chunk := <-chunks:
		reported.WriteString(chunk)
	case <-ctx.Done():
		t.Fatal("stderr chunk was not decoded:", ctx.Err())
	}
	if err := transport.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if got := transport.Stderr(); got != "\ufeff\ufffd\ufffd" {
		t.Errorf("stderr=%q, want preserved BOM and two replacement characters", got)
	}
	for len(chunks) > 0 {
		reported.WriteString(<-chunks)
	}
	if got := reported.String(); got != "\ufeff\ufffd\ufffd" {
		t.Errorf("onStderr=%q, want preserved BOM and two replacement characters", got)
	}
}

// pi v1.4.0 stdio.ts schedules SIGKILL with setTimeout(grace+closeTimeoutMs),
// whose runtime clamps a delay above 2^31-1 to 1ms.
func TestStdioTransportCloseTimeoutOverflowKillsPromptly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stubborn fixture relies on POSIX signals")
	}
	transport := NewStdioTransport(StdioTransportOptions{
		Command: stubbornFixtureBin, CloseTimeoutMs: 1 << 31,
	})
	if err := transport.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	pid := transport.PID()
	if pid <= 0 {
		t.Fatalf("server pid = %d after start", pid)
	}
	var grandchild int
	deadline := time.Now().Add(5 * time.Second)
	for grandchild == 0 {
		fmt.Sscanf(transport.Stderr(), "grandchild %d", &grandchild)
		if time.Now().After(deadline) {
			t.Fatalf("grandchild pid never appeared; stderr = %q", transport.Stderr())
		}
		time.Sleep(10 * time.Millisecond)
	}

	if alive, err := processAlive(pid); err != nil || !alive {
		t.Fatalf("stubborn server is not alive before close: alive=%v err=%v", alive, err)
	}
	select {
	case <-transport.exitCh:
		t.Fatal("server exit channel closed before close")
	default:
	}
	closed := make(chan error, 1)
	go func() { closed <- transport.Close(context.Background()) }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(300 * time.Millisecond):
		killProcessTree(pid, syscall.SIGKILL)
		select {
		case <-closed:
		case <-time.After(2 * time.Second):
			t.Fatal("transport close remained blocked after cleanup kill")
		}
		t.Fatal("SIGKILL timer overflow left the stubborn server running")
	}
	for range 100 {
		alive, err := processAlive(grandchild)
		if err != nil {
			t.Fatalf("probe grandchild: %v", err)
		}
		if !alive {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("grandchild %d survived close", grandchild)
}

// pi v1.5.2 (b4afdddf), transports/lines.ts readStdout: messages are assembled
// from stdout chunks that may split anywhere, including inside a rune, a CRLF
// termination is accepted, and blank lines are skipped.
func TestStdioTransportFramesSplitAndTerminatedLines(t *testing.T) {
	newTransport := func() (*StdioTransport, *[]Message) {
		transport := NewStdioTransport(StdioTransportOptions{})
		var messages []Message
		transport.OnMessage(func(message Message) { messages = append(messages, message) })
		transport.OnError(func(err error) { t.Errorf("unexpected error: %v", err) })
		return transport, &messages
	}

	t.Run("message split across reads", func(t *testing.T) {
		transport, messages := newTransport()
		full := []byte("{\"jsonrpc\":\"2.0\",\"method\":\"m\",\"params\":{\"text\":\"héllo\"}}\n")
		for index := range full {
			transport.handleStdout(full[index : index+1])
		}
		if len(*messages) != 1 {
			t.Fatalf("messages = %d, want the frame split at every byte", len(*messages))
		}
	})

	t.Run("utf8 sequence split across reads", func(t *testing.T) {
		transport, messages := newTransport()
		full := []byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"m\",\"params\":{\"text\":\"日本\"}}\n")
		// Split inside the first multi-byte rune so a per-chunk decode would
		// replace half of it.
		cut := bytes.IndexByte(full, 0xe6) + 1
		transport.handleStdout(full[:cut])
		transport.handleStdout(full[cut:])
		if len(*messages) != 1 {
			t.Fatalf("messages = %d, want 1", len(*messages))
		}
		request, ok := (*messages)[0].(*protocol.JsonRpcRequest)
		if !ok || !strings.Contains(string(request.Params), "日本") {
			t.Fatalf("message = %#v, want the decoded rune", (*messages)[0])
		}
	})

	t.Run("carriage return and blank lines", func(t *testing.T) {
		transport, messages := newTransport()
		transport.handleStdout([]byte("{\"jsonrpc\":\"2.0\",\"method\":\"ping\"}\r\n\n   \n"))
		if len(*messages) != 1 {
			t.Fatalf("messages = %d, want the CRLF frame only", len(*messages))
		}
	})

	t.Run("large message in many chunks", func(t *testing.T) {
		transport, messages := newTransport()
		payload := strings.Repeat("a", 1<<20)
		full := []byte("{\"jsonrpc\":\"2.0\",\"method\":\"m\",\"params\":{\"text\":\"" + payload + "\"}}\n")
		for start := 0; start < len(full); start += 64 * 1024 {
			end := start + 64*1024
			if end > len(full) {
				end = len(full)
			}
			transport.handleStdout(full[start:end])
		}
		if len(*messages) != 1 {
			t.Fatalf("messages = %d, want one 1 MiB frame", len(*messages))
		}
	})
}

// pi v1.4.0 transports/stdio.ts reports a non-whitespace stdout tail as an
// incomplete JSON-RPC message before emitting the child-close notification.
func TestStdioExitReportsUnterminatedOutputBeforeClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	transport := NewStdioTransport(StdioTransportOptions{Command: os.Args[0], Env: map[string]string{"PIER_MCP_UTF8_HELPER": "partial"}})
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	events := make(chan string, 2)
	transport.OnError(func(err error) { events <- err.Error() })
	transport.OnClose(func() { events <- "close" })
	if err := transport.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"MCP stdio server closed with an incomplete JSON-RPC message",
		"close",
	} {
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("event = %q, want %q", got, want)
			}
		case <-ctx.Done():
			t.Fatalf("did not receive event %q: %v", want, ctx.Err())
		}
	}
}

func TestStdioExitDrainsOutputBeforeCloseEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	transport := NewStdioTransport(StdioTransportOptions{Command: os.Args[0], Env: map[string]string{"PIER_MCP_UTF8_HELPER": "exit"}})
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	messages := make(chan string, 1)
	transport.OnMessage(func(message Message) {
		if err := transport.Close(ctx); err != nil {
			messages <- "close error: " + err.Error()
			return
		}
		messages <- message.(*protocol.JsonRpcNotification).Method
	})
	closed := make(chan struct{})
	transport.OnClose(func() { close(closed) })
	if err := transport.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("close notification did not arrive after reentrant Close:", ctx.Err())
	}
	select {
	case method := <-messages:
		if method != "\ufffd\ufffd" {
			t.Errorf("method=%q, want the complete final notification", method)
		}
	default:
		t.Error("OnClose fired before the final notification completed")
	}
	if got := transport.Stderr(); got != "\ufeff\ufffd\ufffd" {
		t.Errorf("OnClose stderr=%q, want complete final stderr", got)
	}
}

// TestStdioFixtureMatchesReferenceBytes pins the fixture's wire output against
// packages/mcp/test/fixtures/stdio-server.mjs, which is the script the reference
// runs. The fixture exists to be byte-faithful, and three things had drifted:
// `String(message.params.arguments.text)` renders a number as its digits and a
// present null as "null" (the port returned "" for both), an absent text
// argument renders "undefined", and the reference writes the line without
// HTML-escaping `<`, `>` and `&`. The id above 2^53 differs on purpose: the
// reference's JSON.stringify renumbers it, and a fixture has no reason to.
func TestStdioFixtureMatchesReferenceBytes(t *testing.T) {
	command := exec.Command(stdioFixtureBin)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"arguments":{"text":"a < b & c > d"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"arguments":{"text":123}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"arguments":{"text":null}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"arguments":{"text":true}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"arguments":{}}}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":9,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":10,"method":"nope"}`,
	}
	want := []string{
		`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"stdio-fixture","version":"1.0.0"}}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"echo","inputSchema":{"type":"object"}}]}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"a < b & c > d"}]}}`,
		`{"jsonrpc":"2.0","id":4,"result":{"content":[{"type":"text","text":"123"}]}}`,
		`{"jsonrpc":"2.0","id":5,"result":{"content":[{"type":"text","text":"null"}]}}`,
		`{"jsonrpc":"2.0","id":6,"result":{"content":[{"type":"text","text":"true"}]}}`,
		`{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"undefined"}]}}`,
		`{"jsonrpc":"2.0","id":8,"result":{"content":[{"type":"text","text":"undefined"}]}}`,
		`{"jsonrpc":"2.0","id":9,"result":{}}`,
		`{"jsonrpc":"2.0","id":10,"error":{"code":-32601,"message":"not found"}}`,
	}
	for _, request := range requests {
		if _, err := io.WriteString(stdin, request+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	_ = stdin.Close()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	var got []string
	for scanner.Scan() {
		got = append(got, scanner.Text())
	}
	_ = command.Wait()
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("line %d:\n got %s\nwant %s", index+1, got[index], want[index])
		}
	}
}

// TestStubbornFixtureMatchesReferenceBytes pins the stubborn fixture against
// packages/mcp/test/fixtures/stubborn-server.mjs. The reference answers every
// `initialize`, including one with no id (writing no id member), and emits
// `{jsonrpc, id, result}` in that order. The port required an id and serialized
// a Go map, which dropped the no-id answer and sorted the keys.
//
// The fixture ignores EOF and SIGTERM and spawns a grandchild, so the test
// configures the process group the way the transport does and kills the group.
func TestStubbornFixtureMatchesReferenceBytes(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		requests []string
		want     []string
	}{
		{
			name: "id then no id",
			requests: []string{
				`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
				`{"jsonrpc":"2.0","method":"initialize"}`,
			},
			want: []string{
				`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"stubborn-fixture","version":"1.0.0"}}}`,
				`{"jsonrpc":"2.0","result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"stubborn-fixture","version":"1.0.0"}}}`,
			},
		},
		{
			name: "string id and an ignored method",
			requests: []string{
				`{"jsonrpc":"2.0","id":"s","method":"initialize"}`,
				`{"jsonrpc":"2.0","id":99,"method":"tools/list"}`,
			},
			want: []string{
				`{"jsonrpc":"2.0","id":"s","result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"stubborn-fixture","version":"1.0.0"}}}`,
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			command := exec.Command(stubbornFixtureBin)
			configureProcessGroup(command)
			stdin, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = stdin.Close()
				if command.Process != nil {
					_ = killProcessGroup(command.Process.Pid, syscall.SIGKILL)
				}
				_ = command.Wait()
			})
			for _, request := range testCase.requests {
				if _, err := io.WriteString(stdin, request+"\n"); err != nil {
					t.Fatal(err)
				}
			}
			scanner := bufio.NewScanner(stdout)
			scanner.Buffer(make([]byte, 1<<20), 1<<20)
			var got []string
			for len(got) < len(testCase.want) && scanner.Scan() {
				got = append(got, scanner.Text())
			}
			if len(got) != len(testCase.want) {
				t.Fatalf("got %d lines, want %d:\n%s", len(got), len(testCase.want), strings.Join(got, "\n"))
			}
			for index := range testCase.want {
				if got[index] != testCase.want[index] {
					t.Errorf("line %d:\n got %s\nwant %s", index+1, got[index], testCase.want[index])
				}
			}
		})
	}
}
