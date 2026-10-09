package durable

import (
	"context"
	"testing"
)

// Port of bash outputWindow forwarding from packages/durable/src/tools/bash.ts at pi v1.1.0 commit cdf79797b.
func TestToolExecutionApiCountsSkippedOutput(t *testing.T) {
	limits := OutputLimits{MaxBytes: 100, MaxLines: 2, Retain: RetainTail}
	reported := &reportedTool{output: NewOutputBuffer(limits), limits: limits}
	progress := NewProgress(func() (int, error) { return 0, nil }, func(error) {}, 0)
	defer progress.Stop()
	api := &toolExecutionApi{reported: reported, progress: progress}
	api.Output([]byte("two\nthree\nfour\n"), &ShellOutputSkip{Bytes: 4, Newlines: 1, EndsWithNewline: true})
	if got := reported.output.Snapshot(); got != (BoundedOutput{Text: "three\nfour\n", DroppedBytes: 8, DroppedLines: 2}) {
		t.Fatalf("retained output = %+v", got)
	}
}

func TestBashToolForwardsSkippedOutputCounts(t *testing.T) {
	env, _ := newToolTestEnv(t)
	windowed := &windowCaptureEnv{
		testToolEnv: env,
		skipped:     &ShellOutputSkip{Bytes: 4, Newlines: 1, EndsWithNewline: true},
	}
	limits := OutputLimits{Retain: RetainTail, MaxBytes: 100, MaxLines: 2}
	api := &fakeToolApi{env: windowed, outputBuffer: NewOutputBuffer(limits)}
	if _, err := CreateBashTool(nil).Execute(JsonObject{"command": "ignored"}, api, context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := api.outputBuffer.Snapshot(); got != (BoundedOutput{Text: "three\nfour\n", DroppedBytes: 8, DroppedLines: 2}) {
		t.Fatalf("bash retained output = %+v", got)
	}
}

func TestBashToolPassesOutputWindowToEnvironment(t *testing.T) {
	env, _ := newToolTestEnv(t)
	window := &ShellOutputWindow{MaxBytes: 4096, MaxLines: 20, MinIntervalMs: 250, BytesPerSecond: 1024}
	captured := &windowCaptureEnv{testToolEnv: env}
	api := &fakeToolApi{env: captured, outputWindow: window}
	if _, err := CreateBashTool(nil).Execute(JsonObject{"command": "ignored"}, api, context.Background()); err != nil {
		t.Fatal(err)
	}
	if captured.window != window {
		t.Fatalf("environment window = %+v, want exact configured window %+v", captured.window, window)
	}
}

type windowCaptureEnv struct {
	*testToolEnv
	window  *ShellOutputWindow
	skipped *ShellOutputSkip
}

func (e *windowCaptureEnv) Exec(_ string, options *ShellExecOptions, ctx context.Context) (ShellExecResult, error) {
	e.window = options.Window
	if options.OnOutput != nil {
		options.OnOutput("two\nthree\nfour\n", ctx, ShellOutputInfo{Stream: "stdout", Skipped: e.skipped})
	}
	return ShellExecResult{ExitCode: 0}, nil
}
