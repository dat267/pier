//go:build unix

package coding

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// pi v1.4.0 (4a080252), utils/clipboard-command.ts abort destroys stdout
// as well as killing the child. A daemonized descendant can retain that pipe
// after its parent exits, so killing the parent alone cannot release a reader.
func TestClipboardReadCancellationClosesInheritedOutputPipe(t *testing.T) {
	testClipboardInheritedPipeCancellation(t, "")
}

func TestClipboardWriteCancellationClosesInheritedInputPipe(t *testing.T) {
	testClipboardInheritedPipeCancellation(t, strings.Repeat("x", 1<<20))
}

func testClipboardInheritedPipeCancellation(t *testing.T, input string) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not installed")
	}
	dir := t.TempDir()
	readyPath, holdPath := filepath.Join(dir, "ready"), filepath.Join(dir, "hold")
	for _, path := range []string{readyPath, holdPath} {
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fd, err := syscall.Open(readyPath, syscall.O_RDWR|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	ready := os.NewFile(uintptr(fd), readyPath)
	defer ready.Close()
	if err := ready.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan []byte, 1)
	var inputText *string
	if input != "" {
		inputText = &input
	}
	go func() {
		// The child waits on a FIFO, not a timed sleep. Its PID is handed
		// through a separate FIFO so cancellation follows pipe inheritance.
		done <- runClipboardCommand(ctx, sh, []string{"-c", `(read -r ignored < "$2") <&0 & printf '%s\n' "$!" > "$1"; read -r parent < "$2"`, "fixture", readyPath, holdPath}, clipboardCommandOptions{input: inputText, timeoutMS: 5000})
	}()
	line, err := bufio.NewReader(ready).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid fixture descendant PID %q: %v", line, err)
	}
	kill := sync.OnceFunc(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	defer kill()
	cancel()
	select {
	case output := <-done:
		if output != nil {
			t.Fatalf("cancelled read succeeded: %q", output)
		}
	case <-time.After(time.Second):
		t.Error("cancelled clipboard command is still waiting on a descendant's inherited pipe")
		kill()
		<-done
	}
}
