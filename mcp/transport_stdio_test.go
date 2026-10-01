package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	for i := 0; i < 100; i++ {
		if err := syscall.Kill(grandchild, 0); err != nil {
			if _, ok := err.(syscall.Errno); ok && err == syscall.ESRCH {
				return
			}
			t.Fatalf("kill(0) err = %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("grandchild %d still alive", grandchild)
}
