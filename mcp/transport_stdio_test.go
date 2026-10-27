package mcp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// Port of packages/mcp/test/stdio.test.ts against Go ports of the upstream
// fixtures (mcp/testdata/stdiofixture, stubbornfixture), built by TestMain.

var (
	stdioFixtureBin    string
	stubbornFixtureBin string
)

func TestMain(m *testing.M) {
	if helper := os.Getenv("PIER_MCP_UTF8_HELPER"); helper != "" {
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
	code := m.Run()
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
