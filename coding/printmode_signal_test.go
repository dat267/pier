package coding

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/offloop"
)

func TestPrintModeReturnStopsSignalWaiter(t *testing.T) {
	// print-mode.ts unregisters its signal callbacks in finally, including when
	// no prompt was supplied. The Go waiter must not outlive the same boundary.
	session, sessions := newPrintSession(t)
	captureStdout(t, func() {
		if code := RunPrintMode(session, sessions, PrintModeOptions{Mode: CLIModeText}); code != 0 {
			t.Fatalf("exit = %d", code)
		}
	})
	stack := make([]byte, 1<<20)
	stack = stack[:runtime.Stack(stack, true)]
	if strings.Contains(string(stack), "coding.RunPrintMode.func") {
		t.Fatalf("print signal waiter survived return:\n%s", stack)
	}
}

func TestPrintModeReturnDisposesRuntime(t *testing.T) {
	// v1.3.0 print-mode.ts disposes on ordinary returns as well as signals.
	session, sessions := newPrintSession(t)
	disposed := false
	unregister := RegisterSessionResourceCleanup(func(id string) {
		if id == session.SessionID() {
			disposed = true
		}
	})
	defer unregister()
	captureStdout(t, func() { RunPrintMode(session, sessions, PrintModeOptions{Mode: CLIModeText}) })
	if !disposed {
		t.Fatal("print runtime resources were not disposed")
	}
}

// v1.3.0 print-mode.ts disposes the runtime before signal exit. Accepted saves
// must survive even if the process is stopped while a provider is streaming.
func TestPrintModeSignalDrainsAcceptedSaves(t *testing.T) {
	if os.Getenv("PIER_PRINT_SIGNAL_HELPER") == "1" {
		dir := os.Getenv("PIER_PRINT_SIGNAL_DIR")
		release := make(chan struct{})
		queue := offloop.New()
		queue.Go(func() { <-release })
		sessions := NewSessionManager(dir, &SessionManagerOptions{SessionDir: dir})
		sessions.SetWriteQueue(queue)
		session, err := NewAgentSession(&SessionConfig{
			Cwd: dir, Sessions: sessions, Model: &ai.Model{ID: "mock", API: ai.APIOpenAIResponses, Provider: "openai", ContextWindow: 200000},
			StreamFn: func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				stream := ai.NewAssistantMessageEventStream()
				go func() {
					fmt.Fprintln(os.Stderr, "PRINT_SIGNAL_READY")
					<-options.Ctx.Done()
					close(release)
					message := createAssistantMessageT("")
					message.StopReason = ai.StopAborted
					stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopAborted, Error: message})
				}()
				return stream
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		code := RunPrintMode(session, sessions, PrintModeOptions{Mode: os.Getenv("PIER_PRINT_SIGNAL_MODE"), InitialMessage: "keep accepted prompt"})
		queue.Stop()
		os.Exit(code)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support sending these POSIX signals")
	}
	t.Setenv("TMPDIR", t.TempDir())
	for _, mode := range []string{CLIModeText, CLIModeJSON} {
		for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP} {
			t.Run(fmt.Sprintf("%s/%d", mode, sig), func(t *testing.T) {
				dir := t.TempDir()
				binary, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, binary, "-test.run=^TestPrintModeSignalDrainsAcceptedSaves$")
				command.Env = append(os.Environ(), "PIER_PRINT_SIGNAL_HELPER=1", "PIER_PRINT_SIGNAL_DIR="+dir, "PIER_PRINT_SIGNAL_MODE="+mode)
				stderr, err := command.StderrPipe()
				if err != nil {
					t.Fatal(err)
				}
				lines := make(chan string, 16)
				if err := command.Start(); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = command.Process.Kill() }()
				go func() {
					defer close(lines)
					scanner := bufio.NewScanner(stderr)
					for scanner.Scan() {
						lines <- scanner.Text()
					}
				}()
				ready := false
				for !ready {
					select {
					case line, ok := <-lines:
						if !ok {
							t.Fatal("helper exited before provider started")
						}
						ready = line == "PRINT_SIGNAL_READY"
					case <-ctx.Done():
						t.Fatal("helper did not start provider")
					}
				}
				if err := command.Process.Signal(sig); err != nil {
					t.Fatal(err)
				}
				err = command.Wait()
				exit, ok := err.(*exec.ExitError)
				wantCode := 143
				if sig == syscall.SIGHUP {
					wantCode = 129
				}
				if !ok || exit.ExitCode() != wantCode {
					t.Fatalf("exit = %v, want %d", err, wantCode)
				}
				files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
				if err != nil || len(files) != 1 {
					t.Fatalf("accepted session not persisted: files=%v err=%v", files, err)
				}
				data, err := os.ReadFile(files[0])
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(data), "keep accepted prompt") {
					t.Fatalf("accepted prompt was lost: %s", data)
				}
			})
		}
	}
}
