package coding

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

type bashExecutorFixtureChunk struct {
	Text   string `json:"text"`
	Repeat *int   `json:"repeat"`
}

type bashExecutorFixtureOperations struct{ chunks []bashExecutorFixtureChunk }

func (operations bashExecutorFixtureOperations) Exec(_ context.Context, _, _ string, options BashExecOptions) (*int, error) {
	for _, chunk := range operations.chunks {
		count := 1
		if chunk.Repeat != nil {
			count = *chunk.Repeat
		}
		options.OnData([]byte(strings.Repeat(chunk.Text, count)))
	}
	code := 0
	return &code, nil
}

type bashExecutorFixtureResult struct {
	OutputSHA256 string `json:"outputSHA256"`
	OutputBytes  int    `json:"outputBytes"`
	Truncated    bool   `json:"truncated"`
	Cancelled    bool   `json:"cancelled"`
	ExitCode     *int   `json:"exitCode"`
	HasFile      bool   `json:"hasFile"`
	FileSHA256   string `json:"fileSHA256"`
}

// pi v1.4.0 core/bash-executor.ts sizes its rolling display chunks with
// JS string.length (UTF-16), not UTF-8 bytes. The real pinned function generates
// output and full-file hashes; no shell commands are executed. Regenerate:
// node coding/testdata/bash-executor/generate.mjs ./pi > coding/testdata/bash-executor/golden.json
func TestBashExecutorRollingOutputMatchesUpstream(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	raw, err := os.ReadFile("testdata/bash-executor/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var tests []struct {
		Name     string                     `json:"name"`
		Chunks   []bashExecutorFixtureChunk `json:"chunks"`
		Expected bashExecutorFixtureResult  `json:"expected"`
	}
	if err := json.Unmarshal(raw, &tests); err != nil {
		t.Fatal(err)
	}
	if len(tests) == 0 {
		t.Fatal("empty bash executor corpus")
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			result, err := ExecuteBashWithOperations(context.Background(), "fixture", t.TempDir(), bashExecutorFixtureOperations{test.Chunks}, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := bashExecutorFixtureResult{
				OutputSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(result.Output))),
				OutputBytes:  len(result.Output), Truncated: result.Truncated, Cancelled: result.Cancelled,
				ExitCode: result.ExitCode, HasFile: result.FullOutputPath != "",
			}
			if got.HasFile {
				full := readFileBytes(t, result.FullOutputPath)
				got.FileSHA256 = fmt.Sprintf("%x", sha256.Sum256(full))
			}
			if !reflect.DeepEqual(got, test.Expected) {
				t.Errorf("result = %+v, want %+v", got, test.Expected)
			}
		})
	}
}

type reentrantBashOperations struct {
	onData     func([]byte)
	nestedDone <-chan struct{}
}

func (operations *reentrantBashOperations) Exec(_ context.Context, _, _ string, options BashExecOptions) (*int, error) {
	operations.onData = options.OnData
	options.OnData([]byte("outer"))
	if operations.nestedDone != nil {
		<-operations.nestedDone
	}
	code := 0
	return &code, nil
}

// pi v1.4.0 calls onChunk synchronously from appendText without a mutex.
// Likewise, arbitrary output callbacks must not run under the Go state lock.
func TestBashOutputCallbackMayReenterOutput(t *testing.T) {
	operations := &reentrantBashOperations{}
	nestedDone := make(chan struct{})
	operations.nestedDone = nestedDone
	nestedRanDuringCallback := false

	result, err := ExecuteBashWithOperations(context.Background(), "fixture", t.TempDir(), operations, &BashExecutorOptions{
		OnChunk: func(chunk string) {
			if chunk != "outer" {
				return
			}
			go func() {
				operations.onData([]byte("nested"))
				close(nestedDone)
			}()
			select {
			case <-nestedDone:
				nestedRanDuringCallback = true
			case <-time.After(100 * time.Millisecond):
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !nestedRanDuringCallback {
		t.Fatal("reentrant OnData could not finish while OnChunk was running")
	}
	if result.Output != "outernested" {
		t.Fatalf("output = %q, want outer nested output in order", result.Output)
	}
}

// pi v1.5.2 (b4afdddf), utils/shell.ts killProcessTree and
// core/tools/bash.ts onAbort: cancelling a shell call kills the process tree,
// including a descendant that still holds the inherited stdout pipe. A port
// that only killed the shell would leave the descendant running (it writes the
// marker) and would block on the pipe the descendant still holds.
func TestBashCancelKillsDescendantsHoldingThePipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell spawns a detached process group only on POSIX")
	}
	cwd := t.TempDir()
	marker := filepath.Join(cwd, "descendant-ran")
	cancel := make(chan struct{})
	go func() {
		time.Sleep(200 * time.Millisecond)
		close(cancel)
	}()
	start := time.Now()
	_, err := CreateLocalBashOperations("", nil).Exec(context.Background(),
		"(sleep 0.6; echo late > "+marker+") & sleep 30", cwd, BashExecOptions{Signal: cancel})
	elapsed := time.Since(start)
	if err == nil || err.Error() != "aborted" {
		t.Fatalf("err = %v, want aborted", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("cancel took %s, want the call to return without waiting on the pipe", elapsed)
	}
	// The descendant would write 0.4s after the cancel; it must not survive.
	time.Sleep(800 * time.Millisecond)
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("descendant survived the cancel and wrote its marker")
	}
}

// linesOnlyBashOperations emits a command's output in one chunk.
type linesOnlyBashOperations struct {
	output string
}

func (operations *linesOnlyBashOperations) Exec(_ context.Context, _, _ string, options BashExecOptions) (*int, error) {
	options.OnData([]byte(operations.output))
	code := 0
	return &code, nil
}

// Reference: pi v1.5.2 (b4afdddf) core/bash-executor.ts ensureTempFile writes
// every buffered chunk into the new file, and finish calls it whenever the tail
// truncates. A command whose output exceeds the line limit but stays under
// DEFAULT_MAX_BYTES therefore gets a full-output file holding all of it. Pier
// created the file without writing the buffered chunks: the write loop only
// runs when new data arrives, so the reported path pointed at an empty file.
func TestExecuteBashWritesTheFullOutputWhenOnlyTheTailTruncates(t *testing.T) {
	var builder strings.Builder
	lineCount := DefaultMaxLines + 500
	for index := range lineCount {
		builder.WriteString("line ")
		builder.WriteString(itoa(index))
		builder.WriteString("\n")
	}
	output := builder.String()
	if len(output) >= DefaultMaxBytes {
		t.Fatalf("fixture is %d bytes, want under the %d-byte raw threshold", len(output), DefaultMaxBytes)
	}

	result, err := ExecuteBashWithOperations(context.Background(), "fixture", t.TempDir(),
		&linesOnlyBashOperations{output: output}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated {
		t.Fatalf("result is not truncated for %d lines", lineCount)
	}
	if result.FullOutputPath == "" {
		t.Fatal("no full-output path for a truncated result")
	}
	t.Cleanup(func() { _ = os.Remove(result.FullOutputPath) })

	saved := readFileBytes(t, result.FullOutputPath)
	if len(saved) == 0 {
		t.Fatal("full-output file is empty")
	}
	if got := strings.Count(string(saved), "\n"); got != lineCount {
		t.Fatalf("full-output file holds %d lines, want %d", got, lineCount)
	}
	if !strings.HasSuffix(string(saved), "line "+itoa(lineCount-1)+"\n") {
		t.Fatal("full-output file does not end with the last line")
	}
}

// Reference: pi v1.5.0 (f9a26365) core/bash-executor.ts tracks `totalBytes` as
// the raw byte length of each chunk (`totalBytes += data.length` in onData) and
// starts the full-output file once it exceeds DEFAULT_MAX_BYTES, before any
// sanitizing: `if (totalBytes > DEFAULT_MAX_BYTES) ensureTempFile()`. A stream
// that is almost entirely ANSI escapes therefore still produces
// `fullOutputPath`, even though its sanitized text is tiny and truncateTail
// reports no truncation.
func TestBashExecutorFullOutputFileUsesRawBytes(t *testing.T) {
	esc := "\x1b"
	zero := 0
	run := func(t *testing.T, chunks []string) BashResult {
		t.Helper()
		result, err := ExecuteBashWithOperations(context.Background(), "cmd", "/tmp",
			&scriptedOps{chunks: chunks, exitCode: &zero}, &BashExecutorOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	// Escape codes only: ~76 KB raw, 2 bytes of text.
	var ansiHeavy string
	for range 9000 {
		ansiHeavy += esc + "[31m" + esc + "[0m"
	}
	ansiHeavy += "hi"
	result := run(t, []string{ansiHeavy})
	if result.FullOutputPath == "" {
		t.Errorf("ANSI-heavy stream has no full-output file; output %q, truncated %v", result.Output, result.Truncated)
	}
	if result.Truncated || result.Output != "hi" {
		t.Errorf("output %q truncated %v, want %q and false", result.Output, result.Truncated, "hi")
	}

	// Controls: the threshold is the raw byte count, strictly greater than
	// DEFAULT_MAX_BYTES, and plain oversized text is still truncated.
	if got := run(t, []string{jsRepeatString("z", DefaultMaxBytes)}); got.FullOutputPath != "" || got.Truncated {
		t.Errorf("exactly DefaultMaxBytes: file %q truncated %v, want none", got.FullOutputPath, got.Truncated)
	}
	if got := run(t, []string{jsRepeatString("z", DefaultMaxBytes+1)}); got.FullOutputPath == "" || !got.Truncated {
		t.Errorf("DefaultMaxBytes+1: file %q truncated %v, want file and truncated", got.FullOutputPath, got.Truncated)
	}
}

func jsRepeatString(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}

// bash.ts reports String(timeout) (pi v1.5.0
// packages/coding-agent/src/core/tools/bash.ts, the "timeout:" error), so
// 1000000 is not Go's %g "1e+06".
func TestBashTimeoutMessageFormatsLikeJavaScript(t *testing.T) {
	for value, want := range map[float64]string{
		1:       "Command timed out after 1 seconds",
		0.5:     "Command timed out after 0.5 seconds",
		1000000: "Command timed out after 1000000 seconds",
		1e21:    "Command timed out after 1e+21 seconds",
	} {
		if got := bashTimeoutMessage(value); got != want {
			t.Errorf("bashTimeoutMessage(%v) = %q; want %q", value, got, want)
		}
	}
}

// TestBashUpdateThrottleCoalesces pins the streamed-update policy ported from
// upstream (updateDirty / lastUpdateAt / BASH_UPDATE_THROTTLE_MS = 100): at
// most one update per interval, whatever the chunk rate. The port emitted one
// snapshot per 64 KB read instead, so a fast command's output flooded the UI
// and every chunk paid a full-output snapshot.
func TestBashUpdateThrottleCoalesces(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	throttle := &bashUpdateThrottle{interval: BashUpdateThrottleMS, now: func() time.Time { return now }}

	// The first chunk emits immediately (no previous update).
	if emitNow, _ := throttle.mark(); !emitNow {
		t.Fatal("the first chunk must emit immediately")
	}
	if !throttle.take() {
		t.Fatal("the first chunk must have a pending update to take")
	}
	if throttle.take() {
		t.Fatal("taking twice must not emit twice")
	}

	// A burst inside the interval is deferred and coalesced into one update.
	emitNow, delay := throttle.mark()
	if emitNow {
		t.Fatal("a chunk inside the interval must not emit immediately")
	}
	if delay <= 0 || delay > BashUpdateThrottleMS {
		t.Fatalf("delay = %v, want (0, %v]", delay, BashUpdateThrottleMS)
	}
	for i := range 100 {
		emitNow, _ := throttle.mark()
		if emitNow {
			t.Fatalf("burst chunk %d emitted inside the interval", i)
		}
	}
	if !throttle.take() {
		t.Fatal("the deferred update must be taken once the timer fires")
	}
	if throttle.take() {
		t.Fatal("the coalesced burst must produce exactly one update")
	}

	// After the interval the next chunk emits immediately again.
	now = now.Add(BashUpdateThrottleMS)
	if emitNow, _ := throttle.mark(); !emitNow {
		t.Fatal("a chunk after the interval must emit immediately")
	}
}
