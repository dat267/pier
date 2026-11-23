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
	// The waiter closes signalDone before its goroutine has fully exited, so the
	// stack can still show it for a scheduler tick after RunPrintMode returns.
	// Poll briefly; a genuinely leaked waiter remains and fails the test.
	deadline := time.Now().Add(2 * time.Second)
	for {
		stack := make([]byte, 1<<20)
		stack = stack[:runtime.Stack(stack, true)]
		if !strings.Contains(string(stack), "coding.RunPrintMode.func") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("print signal waiter survived return:\n%s", stack)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPrintModeReturnDisposesRuntime(t *testing.T) {
	// v1.4.0 print-mode.ts disposes on ordinary returns as well as signals.
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

// v1.4.0 print-mode.ts disposes the runtime before signal exit. Accepted saves
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
				// scanErr is written before close(lines) and read only after
				// the closed channel is received.
				var scanErr error
				go func() {
					defer close(lines)
					scanner := bufio.NewScanner(stderr)
					for scanner.Scan() {
						lines <- scanner.Text()
					}
					scanErr = scanner.Err()
				}()
				ready := false
				for !ready {
					select {
					case line, ok := <-lines:
						if !ok {
							t.Fatalf("helper exited before provider started (stderr read error: %v)", scanErr)
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
				data := readFileBytes(t, files[0])
				if !strings.Contains(string(data), "keep accepted prompt") {
					t.Fatalf("accepted prompt was lost: %s", data)
				}
			})
		}
	}
}

// pi v1.5.2 (b4afdddf) modes/print-mode.ts counts signals and calls
// process.exit(exitCode) on `signalCount > 1`, so a second SIGTERM does not
// wait for disposal. The port's handler read exactly one signal, so a repeated
// one sat in the buffered channel and a stuck drain kept the process alive.
func TestPrintModeRepeatedSignalSkipsTheDrain(t *testing.T) {
	// A stuck write stands in for a drain that never finishes.
	release := make(chan struct{})
	queue := offloop.New()
	queue.Go(func() { <-release })
	defer func() { close(release); queue.Stop() }()

	dir := t.TempDir()
	sessions := NewSessionManager(dir, &SessionManagerOptions{SessionDir: dir})
	sessions.SetWriteQueue(queue)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: sessions, Model: &ai.Model{ID: "mock", API: ai.APIOpenAIResponses, Provider: "openai", ContextWindow: 200000},
		StreamFn: func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				<-options.Ctx.Done()
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

	// Drive the handler through the seam rather than a real signal.
	watchedCh := make(chan chan os.Signal, 1)
	previousNotify := printSignalNotify
	previousStop := printSignalStop
	printSignalNotify = func(ch chan os.Signal) func() {
		watchedCh <- ch
		return func() {}
	}
	printSignalStop = func(chan os.Signal) {}
	t.Cleanup(func() { printSignalNotify, printSignalStop = previousNotify, previousStop })

	// Send the two signals once the handler is subscribed.
	go func() {
		watched := <-watchedCh
		select {
		case watched <- syscall.SIGTERM:
		case <-time.After(time.Second):
			t.Error("first signal not received")
			return
		}
		time.Sleep(200 * time.Millisecond)
		select {
		case watched <- syscall.SIGTERM:
		case <-time.After(time.Second):
			t.Error("second signal not received")
		}
	}()
	codes := make(chan int, 1)
	go func() {
		codes <- RunPrintMode(session, sessions, PrintModeOptions{Mode: CLIModeText, InitialMessage: "prompt"})
	}()
	select {
	case code := <-codes:
		if code != 143 {
			t.Fatalf("exit = %d, want 143", code)
		}
	case <-time.After(5 * time.Second):
		buf := make([]byte, 1<<20)
		buf = buf[:runtime.Stack(buf, true)]
		t.Fatalf("a repeated signal did not end the run:\n%s", buf)
	}
}
