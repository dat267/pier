package cmd

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

func strPtr(s string) *string { return &s }

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

func TestResumeSessionByExactID(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	source := makeSessionFile(t, cwd)

	args := &coding.Args{Session: strPtr(source.GetSessionID())}
	sm, err := resumeSession(args, cwd, t.TempDir(), nil)
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

	args := &coding.Args{Session: strPtr(source.GetSessionID()[:8])}
	sm, err := resumeSession(args, cwd, t.TempDir(), nil)
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

	args := &coding.Args{Session: strPtr(source.GetSessionFile())}
	sm, err := resumeSession(args, cwd, t.TempDir(), nil)
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

	args := &coding.Args{Session: strPtr("does-not-exist-anywhere")}
	_, err := resumeSession(args, cwd, t.TempDir(), nil)
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

	args := &coding.Args{SessionID: strPtr("019463a7-1111-7111-8111-111111111111")}
	sm, err := resumeSession(args, cwd, t.TempDir(), nil)
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

	args := &coding.Args{SessionID: strPtr("019463a7-1111-7111-8111-111111111111"), Continue: true}
	if _, err := resumeSession(args, cwd, t.TempDir(), nil); err == nil {
		t.Fatal("expected an error when --session-id is combined with --continue")
	}
}

func TestResumeSessionIDInvalidFormat(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()

	args := &coding.Args{SessionID: strPtr("bad id!")}
	if _, err := resumeSession(args, cwd, t.TempDir(), nil); err == nil {
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
			args := &coding.Args{SessionID: strPtr("019463a7-1111-7111-8111-111111111111"), Mode: mode}
			var err error
			stdout, stderr := captureStdio(t, func() {
				_, err = resumeSession(args, cwd, t.TempDir(), nil)
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

	args := &coding.Args{Fork: strPtr(source.GetSessionFile())}
	sm, err := resumeSession(args, cwd, t.TempDir(), nil)
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

	args := &coding.Args{Fork: strPtr(source.GetSessionID())}
	sm, err := resumeSession(args, cwd, t.TempDir(), nil)
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

	args := &coding.Args{Fork: strPtr("x"), Continue: true}
	if _, err := resumeSession(args, cwd, t.TempDir(), nil); err == nil {
		t.Fatal("expected an error when --fork is combined with --continue")
	}

	args2 := &coding.Args{Fork: strPtr("x"), NoSession: true}
	if _, err := resumeSession(args2, cwd, t.TempDir(), nil); err == nil {
		t.Fatal("expected an error when --fork is combined with --no-session")
	}
}

func TestResumeSessionForkExistingSessionID(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	source := makeSessionFile(t, cwd)

	args := &coding.Args{Fork: strPtr(source.GetSessionFile()), SessionID: strPtr(source.GetSessionID())}
	if _, err := resumeSession(args, cwd, t.TempDir(), nil); err == nil {
		t.Fatal("expected an error forking with an id that already exists")
	}
}

func TestResumeSessionForkNotFound(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()

	args := &coding.Args{Fork: strPtr("does-not-exist-anywhere")}
	if _, err := resumeSession(args, cwd, t.TempDir(), nil); err == nil {
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
	if _, err := resumeSession(args, cwd, t.TempDir(), nil); !errors.Is(err, errNoSessionSelected) {
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
	sm, err := resumeSession(args, cwd, t.TempDir(), nil)
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

	args := &coding.Args{Session: strPtr(source.GetSessionID())}
	sm, err := resumeSession(args, cwd, t.TempDir(), nil)
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

	args := &coding.Args{Session: strPtr(source.GetSessionID())}
	if _, err := resumeSession(args, cwd, t.TempDir(), nil); !errors.Is(err, errSessionAborted) {
		t.Fatalf("error %v, want errSessionAborted", err)
	}
}

// The --resume picker must not draw on stdout in a headless mode: a JSONL
// consumer parses every stdout line, and upstream keeps the stream clean by
// redirecting stray stdout writes to stderr (takeOverStdout). The port has no
// stdout takeover (D241), so the picker's own terminal is pointed at stderr.
func TestResumePickerKeepsStdoutCleanInHeadlessModes(t *testing.T) {
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
		_, _ = resumeSession(tc.args, cwd, t.TempDir(), nil)
		if sawHeadless != tc.want {
			t.Fatalf("picker headless = %v for %+v, want %v", sawHeadless, tc.args, tc.want)
		}
	}
}
