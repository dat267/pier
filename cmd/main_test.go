package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/coding/interactive"
)

// isolatedAgentDir points PI_CODING_AGENT_DIR at a temp dir so the default
// per-cwd session dirs stay inside the test sandbox.
func isolatedAgentDir(t *testing.T) {
	t.Helper()
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
}

// makeSessionFile writes a real persisted session with a full turn (the file
// flushes only once an assistant message exists, upstream
// flush-on-first-assistant) using the default per-cwd session dir.
func makeSessionFile(t *testing.T, cwd string) *coding.SessionManager {
	t.Helper()
	sm := coding.NewSessionManager(cwd, nil)
	sm.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}})
	sm.AppendMessage(&ai.AssistantMessage{
		API:        ai.APIAnthropicMessages,
		Provider:   "anthropic",
		Model:      "m",
		Content:    ai.ContentList{ai.TextContent{Text: "hi"}},
		StopReason: ai.StopStop,
	})
	return sm
}

// Reference: pi v1.4.0 packages/coding-agent/src/main.ts checks --version
// before the later help handling.
func TestExecuteVersionTakesPriorityOverHelp(t *testing.T) {
	originalArgs := os.Args
	os.Args = []string{"pier", "--help", "--version"}
	defer func() { os.Args = originalArgs }()
	stdout, stderr := captureStdio(t, Execute)
	if stdout != coding.Version+"\n" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q, want version only", stdout, stderr)
	}
}

// Reference: pi v1.4.0 packages/coding-agent/src/main.ts handles --export
// before its later help handling.
func TestExecuteExportTakesPriorityOverHelp(t *testing.T) {
	isolatedAgentDir(t)
	source := makeSessionFile(t, t.TempDir()).GetSessionFile()
	outputPath := filepath.Join(t.TempDir(), "export.html")
	originalArgs := os.Args
	os.Args = []string{"pier", "--help", "--export", source, outputPath}
	defer func() { os.Args = originalArgs }()
	stdout, stderr := captureStdio(t, Execute)
	if !strings.Contains(stdout, "Exported to: "+outputPath) || strings.Contains(stdout, "Usage:") || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("export output: %v", err)
	}
}

// Reference: pi v1.4.0 packages/coding-agent/src/main.ts applies --offline
// before dispatching auth and package commands.
func TestExecuteAppliesOfflineModeBeforeAuthCommand(t *testing.T) {
	isolatedAgentDir(t)
	t.Setenv("PI_OFFLINE", "")
	t.Setenv("PI_SKIP_VERSION_CHECK", "")
	t.Setenv("OPENAI_API_KEY", "test-key")
	originalArgs := os.Args
	os.Args = []string{"pier", "auth", "check", "--provider", "openai", "--offline"}
	defer func() { os.Args = originalArgs }()
	_, stderr := captureStdio(t, Execute)
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	if os.Getenv("PI_OFFLINE") != "1" || os.Getenv("PI_SKIP_VERSION_CHECK") != "1" {
		t.Fatalf("offline flags = %q, %q", os.Getenv("PI_OFFLINE"), os.Getenv("PI_SKIP_VERSION_CHECK"))
	}
}

func TestResumeSessionByExactID(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	source := makeSessionFile(t, cwd)

	args := &coding.Args{Session: new(source.GetSessionID())}
	sm, err := resumeSession(args, cwd, nil)
	if err != nil {
		t.Fatalf("resumeSession: %v", err)
	}
	if sm.GetSessionID() != source.GetSessionID() {
		t.Fatalf("resumed id %s, want %s", sm.GetSessionID(), source.GetSessionID())
	}
	if sm.GetSessionFile() != source.GetSessionFile() {
		t.Fatalf("resumed file %s, want %s", sm.GetSessionFile(), source.GetSessionFile())
	}
}

func TestResumeSessionByIDPrefix(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	source := makeSessionFile(t, cwd)

	args := &coding.Args{Session: new(source.GetSessionID()[:8])}
	sm, err := resumeSession(args, cwd, nil)
	if err != nil {
		t.Fatalf("resumeSession: %v", err)
	}
	if sm.GetSessionID() != source.GetSessionID() {
		t.Fatalf("resumed id %s, want %s", sm.GetSessionID(), source.GetSessionID())
	}
}

func TestResumeSessionByPathStillWorks(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	source := makeSessionFile(t, cwd)

	args := &coding.Args{Session: new(source.GetSessionFile())}
	sm, err := resumeSession(args, cwd, nil)
	if err != nil {
		t.Fatalf("resumeSession: %v", err)
	}
	if sm.GetSessionFile() != source.GetSessionFile() {
		t.Fatalf("resumed file %s, want %s", sm.GetSessionFile(), source.GetSessionFile())
	}
}

func TestResumeSessionNotFound(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()

	args := &coding.Args{Session: new("does-not-exist-anywhere")}
	_, err := resumeSession(args, cwd, nil)
	if err == nil {
		t.Fatal("expected an error for an unmatched --session argument")
	}
	if !strings.Contains(err.Error(), "No session found matching") {
		t.Fatalf("error %q, want it to name the unmatched argument", err)
	}
}

func TestResumeSessionIDCreatesNewSessionWithID(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()

	args := &coding.Args{SessionID: new("019463a7-1111-7111-8111-111111111111")}
	sm, err := resumeSession(args, cwd, nil)
	if err != nil {
		t.Fatalf("resumeSession: %v", err)
	}
	if sm.GetSessionID() != *args.SessionID {
		t.Fatalf("new session id %s, want %s", sm.GetSessionID(), *args.SessionID)
	}
	if !strings.HasSuffix(sm.GetSessionFile(), *args.SessionID+".jsonl") {
		t.Fatalf("session file %s should end with the custom id", sm.GetSessionFile())
	}
}

func TestResumeSessionIDConflict(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()

	args := &coding.Args{SessionID: new("019463a7-1111-7111-8111-111111111111"), Continue: true}
	if _, err := resumeSession(args, cwd, nil); err == nil {
		t.Fatal("expected an error when --session-id is combined with --continue")
	}
}

func TestResumeSessionIDInvalidFormat(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()

	args := &coding.Args{SessionID: new("bad id!")}
	if _, err := resumeSession(args, cwd, nil); err == nil {
		t.Fatal("expected an error for an invalid --session-id format")
	}
}

// captureStdio redirects os.Stdout and os.Stderr for the duration of fn so a
// test can assert which stream a diagnostic used.
func captureStdio(t *testing.T, fn func()) (stdout string, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()
	fn()
	_ = outW.Close()
	_ = errW.Close()
	outBytes, _ := io.ReadAll(outR)
	errBytes, _ := io.ReadAll(errR)
	return string(outBytes), string(errBytes)
}

// Upstream prints the session-id warning with console.error and has already
// taken stdout over in every non-interactive mode, so the warning must never
// reach stdout (a JSONL consumer parses every line).
func TestResumeSessionIDWarningGoesToStderr(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	for _, mode := range []coding.CLIMode{coding.CLIModeText, coding.CLIModeJSON, coding.CLIModeRPC} {
		t.Run(string(mode), func(t *testing.T) {
			args := &coding.Args{SessionID: new("019463a7-1111-7111-8111-111111111111"), Mode: mode}
			var err error
			stdout, stderr := captureStdio(t, func() {
				_, err = resumeSession(args, cwd, nil)
			})
			if err != nil {
				t.Fatalf("resumeSession: %v", err)
			}
			if strings.Contains(stdout, "No project session found") {
				t.Fatalf("%s: warning on stdout: %q", mode, stdout)
			}
			if !strings.Contains(stderr, "No project session found") {
				t.Fatalf("%s: warning not on stderr: stdout=%q stderr=%q", mode, stdout, stderr)
			}
		})
	}
}

func TestResumeSessionFork(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	source := makeSessionFile(t, cwd)

	args := &coding.Args{Fork: new(source.GetSessionFile())}
	sm, err := resumeSession(args, cwd, nil)
	if err != nil {
		t.Fatalf("resumeSession: %v", err)
	}
	if sm.GetSessionID() == source.GetSessionID() {
		t.Fatal("fork must create a new session id")
	}
	header := sm.GetHeader()
	if header == nil || header.ParentSession == nil || *header.ParentSession != source.GetSessionFile() {
		t.Fatalf("fork header parentSession %+v, want %s", header, source.GetSessionFile())
	}
}

func TestResumeSessionForkByID(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	source := makeSessionFile(t, cwd)

	args := &coding.Args{Fork: new(source.GetSessionID())}
	sm, err := resumeSession(args, cwd, nil)
	if err != nil {
		t.Fatalf("resumeSession: %v", err)
	}
	if sm.GetSessionID() == source.GetSessionID() {
		t.Fatal("fork must create a new session id")
	}
}

func TestResumeSessionForkConflict(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()

	args := &coding.Args{Fork: new("x"), Continue: true}
	if _, err := resumeSession(args, cwd, nil); err == nil {
		t.Fatal("expected an error when --fork is combined with --continue")
	}

	args2 := &coding.Args{Fork: new("x"), NoSession: true}
	if _, err := resumeSession(args2, cwd, nil); err == nil {
		t.Fatal("expected an error when --fork is combined with --no-session")
	}
}

func TestResumeSessionForkExistingSessionID(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	source := makeSessionFile(t, cwd)

	args := &coding.Args{Fork: new(source.GetSessionFile()), SessionID: new(source.GetSessionID())}
	if _, err := resumeSession(args, cwd, nil); err == nil {
		t.Fatal("expected an error forking with an id that already exists")
	}
}

func TestResumeSessionForkNotFound(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()

	args := &coding.Args{Fork: new("does-not-exist-anywhere")}
	if _, err := resumeSession(args, cwd, nil); err == nil {
		t.Fatal("expected an error for an unmatched --fork argument")
	}
}

func TestResumeSessionPickerCancelled(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	previous := selectResumeSession
	selectResumeSession = func(cwd, sessionDir string, settings *coding.SettingsManager, headless bool) (string, bool) {
		return "", false
	}
	defer func() { selectResumeSession = previous }()

	args := &coding.Args{Resume: true}
	if _, err := resumeSession(args, cwd, nil); !errors.Is(err, errNoSessionSelected) {
		t.Fatalf("error %v, want errNoSessionSelected", err)
	}
}

func TestResumeSessionPickerSelection(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	source := makeSessionFile(t, cwd)
	previous := selectResumeSession
	selectResumeSession = func(cwd, sessionDir string, settings *coding.SettingsManager, headless bool) (string, bool) {
		return source.GetSessionFile(), true
	}
	defer func() { selectResumeSession = previous }()

	args := &coding.Args{Resume: true}
	sm, err := resumeSession(args, cwd, nil)
	if err != nil {
		t.Fatalf("resumeSession: %v", err)
	}
	if sm.GetSessionID() != source.GetSessionID() {
		t.Fatalf("resumed id %s, want %s", sm.GetSessionID(), source.GetSessionID())
	}
}

func TestResumeSessionGlobalPromptsForkOnConfirm(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	otherCwd := t.TempDir()
	source := makeSessionFile(t, otherCwd)
	previous := promptConfirm
	promptConfirm = func(message string) bool { return true }
	defer func() { promptConfirm = previous }()

	args := &coding.Args{Session: new(source.GetSessionID())}
	sm, err := resumeSession(args, cwd, nil)
	if err != nil {
		t.Fatalf("resumeSession: %v", err)
	}
	if sm.GetSessionID() == source.GetSessionID() {
		t.Fatal("confirming the fork prompt must fork, not resume")
	}
	if sm.GetCwd() != cwd {
		t.Fatalf("forked cwd %s, want the current cwd %s", sm.GetCwd(), cwd)
	}
	header := sm.GetHeader()
	if header == nil || header.ParentSession == nil || *header.ParentSession != source.GetSessionFile() {
		t.Fatalf("fork header parentSession %+v, want %s", header, source.GetSessionFile())
	}
}

func TestResumeSessionGlobalPromptsForkOnDecline(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	otherCwd := t.TempDir()
	source := makeSessionFile(t, otherCwd)
	previous := promptConfirm
	promptConfirm = func(message string) bool { return false }
	defer func() { promptConfirm = previous }()

	args := &coding.Args{Session: new(source.GetSessionID())}
	if _, err := resumeSession(args, cwd, nil); !errors.Is(err, errSessionAborted) {
		t.Fatalf("error %v, want errSessionAborted", err)
	}
}

// The --resume picker must not draw on stdout in a headless mode: a JSONL
// consumer parses every stdout line, and upstream keeps the stream clean by
// redirecting stray stdout writes to stderr (takeOverStdout). The port has no
// stdout takeover (D241), so the picker's own terminal is pointed at stderr.
func TestResumePickerKeepsStdoutCleanInHeadlessModes(t *testing.T) {
	previousIsTerminal := sessionPickerIsTerminal
	sessionPickerIsTerminal = func(int) bool { return true }
	defer func() { sessionPickerIsTerminal = previousIsTerminal }()

	if got := sessionPickerOutput(true); got != os.Stderr {
		t.Fatalf("headless picker output = %v, want stderr", got)
	}
	if got := sessionPickerOutput(false); got != os.Stdout {
		t.Fatalf("interactive picker output = %v, want stdout", got)
	}

	isolatedAgentDir(t)
	cwd := t.TempDir()
	previous := selectResumeSession
	var sawHeadless bool
	selectResumeSession = func(cwd, sessionDir string, settings *coding.SettingsManager, headless bool) (string, bool) {
		sawHeadless = headless
		return "", false
	}
	defer func() { selectResumeSession = previous }()

	for _, tc := range []struct {
		args *coding.Args
		want bool
	}{
		{&coding.Args{Resume: true}, false},
		{&coding.Args{Resume: true, Mode: coding.CLIModeJSON}, true},
		{&coding.Args{Resume: true, Print: true}, true},
	} {
		sawHeadless = false
		_, _ = resumeSession(tc.args, cwd, nil)
		if sawHeadless != tc.want {
			t.Fatalf("picker headless = %v for %+v, want %v", sawHeadless, tc.args, tc.want)
		}
	}
}

// pi v1.4.0 main.ts selects print mode whenever either standard stream is not
// a TTY; its stdout takeover keeps the resume picker off the JSON/text stream.
func TestResumePickerUsesStderrWhenPrintModeIsInferred(t *testing.T) {
	previousIsTerminal := sessionPickerIsTerminal
	sessionPickerIsTerminal = func(int) bool { return false }
	defer func() { sessionPickerIsTerminal = previousIsTerminal }()

	isolatedAgentDir(t)
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previousStdout := os.Stdout
	os.Stdout = writePipe
	defer func() {
		os.Stdout = previousStdout
		_ = writePipe.Close()
		_ = readPipe.Close()
	}()

	previous := selectResumeSession
	var sawHeadless bool
	selectResumeSession = func(cwd, sessionDir string, settings *coding.SettingsManager, headless bool) (string, bool) {
		sawHeadless = headless
		return "", false
	}
	defer func() { selectResumeSession = previous }()

	_, _ = resumeSession(&coding.Args{Resume: true}, t.TempDir(), nil)
	if !sawHeadless {
		t.Fatal("resume picker used stdout although the non-TTY stream selects print mode")
	}
}

// Reference: pi v1.4.0 packages/coding-agent/src/main.ts createSessionManager
// resolves --session before --continue, so a specific session wins over the
// convenience flag.
func TestResumeSessionPrefersSessionOverContinue(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	older := makeSessionFile(t, cwd)
	newer := makeSessionFile(t, cwd)
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older.GetSessionFile(), past, past); err != nil {
		t.Fatal(err)
	}
	if newer.GetSessionID() == older.GetSessionID() {
		t.Fatal("fixture sessions share an id")
	}

	args := &coding.Args{Continue: true, Session: new(older.GetSessionID())}
	sm, err := resumeSession(args, cwd, nil)
	if err != nil {
		t.Fatalf("resumeSession: %v", err)
	}
	if sm.GetSessionID() != older.GetSessionID() {
		t.Fatalf("resumed %s, want the requested --session %s", sm.GetSessionID(), older.GetSessionID())
	}
}

// Reference: pi v1.4.0 createSessionManager shows the picker for --resume
// before falling through to --continue.
func TestResumeSessionPrefersResumePickerOverContinue(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	older := makeSessionFile(t, cwd)
	newer := makeSessionFile(t, cwd)
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older.GetSessionFile(), past, past); err != nil {
		t.Fatal(err)
	}
	if newer.GetSessionID() == older.GetSessionID() {
		t.Fatal("fixture sessions share an id")
	}

	// The picker selects the older session; --continue alone would pick newer.
	previous := selectResumeSession
	selectResumeSession = func(_ string, _ string, _ *coding.SettingsManager, _ bool) (string, bool) {
		return older.GetSessionFile(), true
	}
	defer func() { selectResumeSession = previous }()

	args := &coding.Args{Continue: true, Resume: true}
	sm, err := resumeSession(args, cwd, nil)
	if err != nil {
		t.Fatalf("resumeSession: %v", err)
	}
	if sm.GetSessionID() != older.GetSessionID() {
		t.Fatalf("resumed %s, want the picker selection %s", sm.GetSessionID(), older.GetSessionID())
	}
}

// Reference: pi v1.4.0 createSessionManager returns an in-memory manager when
// --no-session is set, before resolving --continue/--resume/--session, so an
// ephemeral run never opens (or appends to) a stored session.
func TestResumeSessionHonorsNoSessionOverContinue(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	recent := makeSessionFile(t, cwd)
	before, err := os.ReadFile(recent.GetSessionFile())
	if err != nil {
		t.Fatal(err)
	}

	args := &coding.Args{NoSession: true, Continue: true}
	sm, err := resumeSession(args, cwd, nil)
	if err != nil {
		t.Fatalf("resumeSession: %v", err)
	}
	if sm.GetSessionFile() != "" {
		t.Fatalf("ephemeral run opened %q", sm.GetSessionFile())
	}
	after, err := os.ReadFile(recent.GetSessionFile())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("ephemeral run modified the stored session")
	}
}

// Match upstream SessionManager.inMemory(cwd, { id }) for --no-session with
// --session-id: the id is used, but nothing is persisted.
func TestResumeSessionNoSessionKeepsRequestedID(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	args := &coding.Args{NoSession: true, SessionID: new("ephemeral-id")}
	sm, err := resumeSession(args, cwd, nil)
	if err != nil {
		t.Fatalf("resumeSession: %v", err)
	}
	if sm.GetSessionID() != "ephemeral-id" || sm.GetSessionFile() != "" {
		t.Fatalf("session id=%q file=%q", sm.GetSessionID(), sm.GetSessionFile())
	}
}

// pi v1.5.0 main.ts collects startup settings diagnostics and reports them
// before running (stderr outside interactive mode); the port drained them only
// for --list-models, so an invalid settings file booted silently.
func TestStartupReportsInvalidSettings(t *testing.T) {
	isolatedAgentDir(t)
	agentDir := os.Getenv("PI_CODING_AGENT_DIR")
	if err := os.WriteFile(filepath.Join(agentDir, "settings.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	args := coding.ParseArgs([]string{
		"--offline", "--no-mcp", "--no-session", "--no-tools",
		"--model", "openai/gpt-4o", "-p", "hi",
	})
	_, stderr := captureStdio(t, func() { _ = run("pier", args) })
	if !strings.Contains(stderr, "Invalid") || !strings.Contains(stderr, "settings") {
		t.Fatalf("stderr = %q; want the invalid settings diagnostic", stderr)
	}
}

// v1.5.0 (f9a26365) main.ts, from 88ffcdc73 (#11511), catches ENOENT from
// process.cwd() and prints the recovery hint, ending in a period.
func TestDescribeGetwdError(t *testing.T) {
	if err := describeGetwdError(&os.PathError{Op: "getwd", Path: ".", Err: os.ErrNotExist}); err == nil ||
		err.Error() != "The current working directory no longer exists. Change to an existing directory and try again." {
		t.Fatalf("err = %v", err)
	}
	if err := describeGetwdError(os.ErrPermission); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("permission error changed: %v", err)
	}
}

// AGENTS.md: tests use temporary agent state, never the real agent directory.
// These child processes build their environment from scratch, so the agent dir
// must be passed explicitly; without it the child reads the developer's real
// ~/.pi/agent, whose session directory can be hundreds of megabytes.
func TestChildProcessesUseTheIsolatedAgentDir(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	want := filepath.Clean(agentDir)

	cases := []struct {
		name   string
		helper string
		env    []string
	}{
		{"run failure", "TestRunFailureCLIHelper", []string{"PIER_TEST_RUN_FAILURE_ARGS=--offline -p hi", "TERM=xterm-256color"}},
		{"unknown flag", "TestUnknownFlagCLIHelper", []string{"PIER_TEST_UNKNOWN_FLAG_ARGS=--offline --bogus"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), childProcessTimeout)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+testCase.helper+"$")
			command.Env = childAgentEnv(append(append([]string{}, testCase.env...), "PIER_TEST_PRINT_AGENT_DIR=1")...)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("child failed: %v: %s", err, output)
			}
			var got string
			for line := range strings.SplitSeq(string(output), "\n") {
				if value, ok := strings.CutPrefix(line, "AGENTDIR="); ok {
					got = filepath.Clean(value)
				}
			}
			if got != want {
				t.Fatalf("child agent dir = %q, want the isolated %q (it reads the developer's real agent directory otherwise)", got, want)
			}
		})
	}
}

// Reference: pi v1.5.2 (b4afdddf) main.ts prints
// `Session already exists with id '...'` with console.error and no "Error: "
// prefix, like the "No session found matching ..." line. Pier returned a plain
// error, so Execute added the prefix.
func TestForkSessionIDCollisionIsABareLine(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	existing := makeSessionFile(t, cwd)
	id := existing.GetSessionID()

	args := &coding.Args{Fork: new(existing.GetSessionFile()), SessionID: &id}
	_, err := resumeSession(args, cwd, nil)
	if err == nil {
		t.Fatal("expected the id collision")
	}
	if _, ok := errors.AsType[bareErrorLine](err); !ok {
		t.Fatalf("err %v is not a bare line", err)
	}
	if err.Error() != "Session already exists with id '"+id+"'" {
		t.Fatalf("err = %q", err.Error())
	}
}

// TestInstallBrowserOpener checks the CLI wires a browser opener, so the OAuth
// login flow opens the authorization URL (upstream utils/open-browser.ts).
func TestInstallBrowserOpener(t *testing.T) {
	previous := interactive.BrowserOpener()
	t.Cleanup(func() { interactive.SetBrowserOpener(previous) })
	interactive.SetBrowserOpener(nil)
	installBrowserOpener()
	if interactive.BrowserOpener() == nil {
		t.Fatal("the browser opener must be installed")
	}
}

// D538: the first session gets the backgrounding threshold from the setting.
// Only session switches passed it before, so a fresh pier ran every long call in
// the foreground until /new or /resume. Print mode keeps it off.
func TestStartupSessionBackgroundsToolCalls(t *testing.T) {
	settings := coding.NewInMemorySettingsManager(&coding.Settings{BackgroundToolCalls: new("2s")}, coding.SettingsManagerCreateOptions{})
	threshold := backgroundToolCallsFor(false, settings)
	if threshold == nil || threshold() != 2*time.Second {
		t.Fatalf("interactive and RPC sessions must read the backgroundToolCalls setting")
	}
	if backgroundToolCallsFor(true, settings) != nil {
		t.Fatal("print mode must not background tool calls: no later turn wakes")
	}
}
