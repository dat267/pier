package coding

// Print mode (single-shot), port of v1.3.0 modes/print-mode.ts. Extensions and
// runtime rebind remain out of scope (D41). JSON shaping lives in jsonevent.go.

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"

	"github.com/dat267/pier/ai"
)

// PrintModeOptions configures RunPrintMode.
type PrintModeOptions struct {
	// Mode is CLIModeText (final response only) or CLIModeJSON (event stream).
	Mode CLIMode
	// InitialMessage is the first prompt, with @file content already expanded.
	InitialMessage string
	// InitialImages attach only to InitialMessage, like upstream print-mode.ts.
	InitialImages []ai.ImageContent
	// Messages are additional prompts sent after the initial one.
	Messages []string
}

// printStdout serializes writes from the agent and caller. Each JSONL record
// flushes immediately; a slow consumer backpressures the producer.
type printStdout struct {
	mu sync.Mutex
	w  *bufio.Writer
}

func (p *printStdout) writeLine(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = p.w.WriteString(line)
	_ = p.w.WriteByte('\n')
	_ = p.w.Flush()
}

func (p *printStdout) flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.w.Flush()
}

// RunPrintMode returns 0 on success, 1 on prompt/persistence failure,
// or 129/143 on SIGHUP/SIGTERM. The caller exits only after its queues drain.
func RunPrintMode(session *AgentSession, sessions *SessionManager, options PrintModeOptions) (exitCode int) {
	ctx, cancel := context.WithCancel(context.Background())
	out := &printStdout{w: bufio.NewWriter(os.Stdout)}
	signals := make(chan os.Signal, 1)
	if runtime.GOOS == "windows" {
		signal.Notify(signals, syscall.SIGTERM)
	} else {
		signal.Notify(signals, syscall.SIGTERM, syscall.SIGHUP)
	}
	signalDone := make(chan struct{})
	signalCode := 0 // Read only after signalDone, including ordinary returns.
	go func() {
		defer close(signalDone)
		select {
		case sig := <-signals:
			signalCode = 143
			if sig == syscall.SIGHUP {
				signalCode = 129
			}
			cancel()
		case <-ctx.Done():
		}
	}()
	var unsubscribe func()
	defer func() {
		signal.Stop(signals)
		cancel()
		<-signalDone
		// Do not exit from a signal goroutine: cancellation must reap tool children
		// and finish event producers before accepted saves and output are drained.
		session.Abort(context.Background())
		if unsubscribe != nil {
			unsubscribe()
		}
		session.Dispose()
		// D210: queue completion alone does not prove that the transcript was saved.
		writeErr := sessions.FlushWrites()
		for _, failure := range sessions.DrainWriteErrors() {
			fmt.Fprintln(os.Stderr, failure.String())
		}
		if writeErr != nil {
			exitCode = 1
		}
		out.flush()
		if signalCode != 0 {
			exitCode = signalCode
		}
	}()

	if options.Mode == CLIModeJSON {
		if header := sessions.GetHeader(); header != nil {
			if encoded, err := MarshalFileEntry(FileEntry{Header: header}); err == nil {
				out.writeLine(strings.TrimSuffix(encoded, "\n"))
			}
		}
		unsubscribe = session.Subscribe(func(event *SessionEvent) {
			encoded, err := MarshalJSONSessionEvent(event)
			if err != nil {
				fmt.Fprintln(os.Stderr, "json event error: "+err.Error())
				return
			}
			out.writeLine(string(encoded))
		})
	}
	prompt := func(text string, images []ai.ImageContent) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := session.Prompt(ctx, text, &PromptOptions{Images: images}); err != nil {
			return err
		}
		return session.WaitForIdle(ctx)
	}
	runErr := func() error {
		if options.InitialMessage != "" {
			if err := prompt(options.InitialMessage, options.InitialImages); err != nil {
				return err
			}
		}
		for _, message := range options.Messages {
			if err := prompt(message, nil); err != nil {
				return err
			}
		}
		return nil
	}()
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		return 1
	}

	if options.Mode == CLIModeText {
		messages := session.Agent.State().Messages
		if len(messages) > 0 {
			if assistant, ok := messages[len(messages)-1].(*ai.AssistantMessage); ok {
				if assistant.StopReason == ai.StopError || assistant.StopReason == ai.StopAborted {
					message := ""
					if assistant.ErrorMessage != nil {
						message = *assistant.ErrorMessage
					}
					if message == "" {
						message = "Request " + string(assistant.StopReason)
					}
					fmt.Fprintln(os.Stderr, message)
					return 1
				}
				for _, block := range assistant.Content {
					if text, ok := block.(ai.TextContent); ok {
						out.writeLine(text.Text)
					}
				}
				if assistant.StopReason == ai.StopLength {
					fmt.Fprintln(os.Stderr, "Response was truncated: the model reached its output token limit.")
					exitCode = 1
				}
			}
		}
	}
	return exitCode
}
