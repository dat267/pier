package coding

import (
	ctxpkg "context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Round 94 unit tests: event bus, usage totals, cache stats, session cwd,
// bash executor and the ANSI stripper.

func (p fixedPrices) GetCachePrice(provider, modelID string) *ModelCachePrice {
	price, ok := p[provider+"/"+modelID]
	if !ok {
		return nil
	}
	return &ModelCachePrice{CacheRead: price}
}

func (s stubSessionCwd) GetCwd() string { return s.cwd }

func TestStripAnsi(t *testing.T) {
	cases := map[string]string{
		"plain":                          "plain",
		"\x1b[31mred\x1b[0m":             "red",
		"\x1b[1;32mbold green\x1b[m":     "bold green",
		"a\x1b]0;title\x07b":             "ab",
		"a\x1b]8;;https://x\x1b\\b":      "ab",
		"no escape \x9b":                 "no escape \x9b",
		"cursor\x1b[2Kgone":              "cursorgone",
		"\x1b[38:5:196mtruecolor\x1b[0m": "truecolor",
	}
	for input, want := range cases {
		if got := StripAnsi(input); got != want {
			t.Errorf("StripAnsi(%q) = %q, want %q", input, got, want)
		}
	}
}

// scriptedOps is a fake BashOperations backend.
type scriptedOps struct {
	chunks   []string
	exitCode *int
	err      error
	command  string
	cwd      string
	onRun    func(options BashExecOptions)
}

func (o *scriptedOps) Exec(ctx ctxpkg.Context, command, cwd string, options BashExecOptions) (*int, error) {
	o.command = command
	o.cwd = cwd
	if o.onRun != nil {
		o.onRun(options)
	}
	for _, chunk := range o.chunks {
		if options.OnData != nil {
			options.OnData([]byte(chunk))
		}
	}
	return o.exitCode, o.err
}

// Upstream bash.ts routes streamed chunks through OutputAccumulator's TextDecoder.
func TestExecuteBashWithOperationsDecodesStreamingUTF8(t *testing.T) {
	code := 0
	ops := &scriptedOps{chunks: []string{string([]byte{0xe2}), string([]byte{0x82, 0xac})}, exitCode: &code}
	result, err := ExecuteBashWithOperations(ctxpkg.Background(), "cmd", "/tmp", ops, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "€" {
		t.Fatalf("split UTF-8 output = %q; want euro sign", result.Output)
	}
}

func TestExecuteBashWithOperations(t *testing.T) {
	code := 0
	ops := &scriptedOps{chunks: []string{"\x1b[31mhello\x1b[0m", " world\r\n"}, exitCode: &code}
	var streamed []string
	result, err := ExecuteBashWithOperations(ctxpkg.Background(), "echo hi", "/tmp", ops, &BashExecutorOptions{
		OnChunk: func(chunk string) { streamed = append(streamed, chunk) },
	})
	if err != nil {
		t.Fatal(err)
	}
	// ANSI is stripped and \r removed from the graded output.
	if result.Output != "hello world\n" {
		t.Fatalf("output = %q", result.Output)
	}
	if len(streamed) != 2 || streamed[0] != "hello" {
		t.Fatalf("streamed = %q", streamed)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 || result.Truncated || result.Cancelled {
		t.Fatalf("result = %+v", result)
	}
	if ops.command != "echo hi" || ops.cwd != "/tmp" {
		t.Fatalf("ops = %+v", ops)
	}

	// A nil exit code from the backend is preserved.
	ops = &scriptedOps{chunks: []string{"partial"}, exitCode: nil}
	result, err = ExecuteBashWithOperations(ctxpkg.Background(), "cmd", "/tmp", ops, nil)
	if err != nil || result.ExitCode != nil {
		t.Fatalf("result = %+v err = %v", result, err)
	}

	// A backend error propagates.
	boom := errors.New("spawn failed")
	ops = &scriptedOps{err: boom}
	if _, err := ExecuteBashWithOperations(ctxpkg.Background(), "cmd", "/tmp", ops, nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}

	// A closed signal channel cancels the result.
	signal := make(chan struct{})
	close(signal)
	ops = &scriptedOps{chunks: []string{"before cancel"}, exitCode: &code}
	result, err = ExecuteBashWithOperations(ctxpkg.Background(), "cmd", "/tmp", ops, &BashExecutorOptions{Signal: signal})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Cancelled || result.ExitCode != nil {
		t.Fatalf("result = %+v", result)
	}
	// A cancelled backend error is not an error.
	ops = &scriptedOps{err: errors.New("aborted"), chunks: []string{"x"}}
	result, err = ExecuteBashWithOperations(ctxpkg.Background(), "cmd", "/tmp", ops, &BashExecutorOptions{Signal: signal})
	if err != nil || !result.Cancelled {
		t.Fatalf("result = %+v err = %v", result, err)
	}
	if _, err := ExecuteBashWithOperations(ctxpkg.Background(), "cmd", "/tmp", nil, nil); err == nil {
		t.Fatal("missing operations must error")
	}
}

func TestExecuteBashStripsANSISequencesSplitAcrossChunks(t *testing.T) {
	code := 0
	ops := &scriptedOps{chunks: []string{"before\x1b[3", "1mred\x1b[0", "m after"}, exitCode: &code}
	var streamed []string
	result, err := ExecuteBashWithOperations(ctxpkg.Background(), "cmd", "/tmp", ops, &BashExecutorOptions{
		OnChunk: func(chunk string) { streamed = append(streamed, chunk) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "beforered after" {
		t.Fatalf("output = %q; want ANSI-stripped text", result.Output)
	}
	if got := strings.Join(streamed, ""); got != "beforered after" {
		t.Fatalf("streamed = %q; want ANSI-stripped text", got)
	}

	ops = &scriptedOps{chunks: []string{"before\x1b]8;;https://example.test", "\x07linked\x1b]8;;\x07after"}, exitCode: &code}
	result, err = ExecuteBashWithOperations(ctxpkg.Background(), "cmd", "/tmp", ops, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "beforelinkedafter" {
		t.Fatalf("OSC output = %q; want hyperlinks stripped across chunks", result.Output)
	}
}

func TestExecuteBashTruncationAndTempFile(t *testing.T) {
	code := 0
	// One oversized line exceeding the 50KB limit forces tail truncation and a
	// temp file with the full output.
	big := strings.Repeat("x", DefaultMaxBytes+5000)
	ops := &scriptedOps{chunks: []string{big}, exitCode: &code}
	result, err := ExecuteBashWithOperations(ctxpkg.Background(), "cmd", "/tmp", ops, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || result.FullOutputPath == "" {
		t.Fatalf("result = %+v", result)
	}
	defer os.Remove(result.FullOutputPath)
	written, err := os.ReadFile(result.FullOutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != big {
		t.Fatalf("temp file length = %d want %d", len(written), len(big))
	}
	if len(result.Output) >= len(big) {
		t.Fatalf("output not truncated: %d", len(result.Output))
	}
}

func TestExecuteBashLocal(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no shell available")
	}
	result, err := ExecuteBash(ctxpkg.Background(), "printf 'out'; printf 'err' >&2", t.TempDir(), &BashExecutorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("result = %+v", result)
	}
	if result.Output != "outerr" && result.Output != "errout" {
		t.Fatalf("output = %q", result.Output)
	}

	// A failing command reports its code.
	result, err = ExecuteBash(ctxpkg.Background(), "exit 3", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode == nil || *result.ExitCode != 3 {
		t.Fatalf("result = %+v", result)
	}

	// A missing working directory fails like upstream.
	if _, err := ExecuteBash(ctxpkg.Background(), "true", filepath.Join(t.TempDir(), "gone"), nil); err == nil ||
		!strings.Contains(err.Error(), "Working directory does not exist:") {
		t.Fatalf("err = %v", err)
	}

	// Cancellation kills the command mid-flight.
	stop := make(chan struct{})
	cancelTimer := time.AfterFunc(200*time.Millisecond, func() { close(stop) })
	start := time.Now()
	result, err = ExecuteBash(ctxpkg.Background(), "sleep 10", t.TempDir(), &BashExecutorOptions{Signal: stop})
	cancelTimer.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if !result.Cancelled || result.ExitCode != nil {
		t.Fatalf("result = %+v", result)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("cancellation took %v", elapsed)
	}
}

func TestLocalShellOperationsTimeout(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no shell available")
	}
	// A cancelled context produces the aborted error.
	ctx, cancel := ctxpkg.WithCancel(ctxpkg.Background())
	cancel()
	ops := CreateLocalBashOperations("", nil)
	timeout := 50 * time.Millisecond
	if _, err := ops.Exec(ctx, "true", t.TempDir(), BashExecOptions{Timeout: &timeout}); err == nil {
		t.Fatal("cancelled context must error")
	}

	// A timeout reports the upstream timeout:<seconds> error, which carries the
	// caller's seconds value (core/tools/bash.ts ShellTimeoutError).
	start := time.Now()
	_, err := ops.Exec(ctxpkg.Background(), "sleep 5", t.TempDir(), BashExecOptions{Timeout: &timeout})
	if err == nil || !strings.HasPrefix(err.Error(), "timeout:0.05") {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("timeout took %v", elapsed)
	}
}

func TestTempFileID(t *testing.T) {
	seen := map[string]bool{}
	for range 64 {
		id := tempFileID()
		if len(id) != 16 {
			t.Fatalf("id = %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

// pi v1.5.2 b4afdddf core/tools/bash.ts createLocalShellOperations.exec calls
// resolveTimeoutMs first, so a non-positive or too large timeout fails before
// the shell runs, with the same messages the bash tool reports.
func TestLocalShellOperationsRejectsInvalidTimeout(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no shell available")
	}
	ops := CreateLocalBashOperations("", nil)
	zero := time.Duration(0)
	_, err := ops.Exec(ctxpkg.Background(), "true", t.TempDir(), BashExecOptions{Timeout: &zero})
	if err == nil || err.Error() != "Invalid timeout: must be a finite number of seconds" {
		t.Fatalf("zero timeout err = %v", err)
	}
	huge := time.Duration(maxTimeoutMS+1000) * time.Millisecond
	_, err = ops.Exec(ctxpkg.Background(), "true", t.TempDir(), BashExecOptions{Timeout: &huge})
	if err == nil || !strings.Contains(err.Error(), "Invalid timeout: maximum is") {
		t.Fatalf("huge timeout err = %v", err)
	}
}
