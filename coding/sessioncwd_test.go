package coding

import (
	"os"
	"path/filepath"
	"testing"
)

type sessionCwdStub struct {
	cwd         string
	sessionFile string
}

func (s sessionCwdStub) GetCwd() string         { return s.cwd }
func (s sessionCwdStub) GetSessionFile() string { return s.sessionFile }

// TestMissingSessionCwdIssueAndError pins pi v1.5.2 (b4afdddf)
// core/session-cwd.ts getMissingSessionCwdIssue and formatMissingSessionCwdError:
// the issue needs a session file and an absent stored cwd, and the CLI error
// names both directories.
func TestMissingSessionCwdIssueAndError(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "gone")
	if err := os.RemoveAll(missing); err != nil {
		t.Fatal(err)
	}

	if issue := GetMissingSessionCwdIssue(sessionCwdStub{cwd: missing}, dir); issue != nil {
		t.Fatalf("no session file must report no issue, got %+v", issue)
	}
	if issue := GetMissingSessionCwdIssue(sessionCwdStub{sessionFile: "/tmp/s.jsonl"}, dir); issue != nil {
		t.Fatalf("an empty stored cwd must report no issue, got %+v", issue)
	}
	if issue := GetMissingSessionCwdIssue(sessionCwdStub{cwd: dir, sessionFile: "/tmp/s.jsonl"}, dir); issue != nil {
		t.Fatalf("an existing cwd must report no issue, got %+v", issue)
	}

	issue := GetMissingSessionCwdIssue(sessionCwdStub{cwd: missing, sessionFile: "/tmp/s.jsonl"}, dir)
	if issue == nil {
		t.Fatal("a missing stored cwd must report an issue")
	}
	if issue.SessionCwd != missing || issue.SessionFile != "/tmp/s.jsonl" || issue.FallbackCwd != dir {
		t.Fatalf("issue = %+v", issue)
	}
	want := "Stored session working directory does not exist: " + missing +
		"\nSession file: /tmp/s.jsonl\nCurrent working directory: " + dir
	if got := FormatMissingSessionCwdError(*issue); got != want {
		t.Errorf("error text =\n%q\nwant\n%q", got, want)
	}
	if err := AssertSessionCwdExists(sessionCwdStub{cwd: missing, sessionFile: "/tmp/s.jsonl"}, dir); err == nil {
		t.Fatal("AssertSessionCwdExists accepted a missing cwd")
	} else if err.Error() != want {
		t.Errorf("asserted error = %q, want %q", err.Error(), want)
	}
}

// TestMissingSessionCwdPromptExplainsTheFallback pins D476: pier's prompt body
// names both directories instead of the reference's one-line
// "Session cwd does not exist: <cwd>", because the app's fallback confirm has
// only Yes/No labels while the reference always shows the labelled selector.
func TestMissingSessionCwdPromptExplainsTheFallback(t *testing.T) {
	issue := SessionCwdIssue{SessionFile: "/tmp/s.jsonl", SessionCwd: "/old/dir", FallbackCwd: "/new/dir"}
	want := "cwd from session file does not exist\n/old/dir\n\ncontinue in current cwd\n/new/dir"
	if got := FormatMissingSessionCwdPrompt(issue); got != want {
		t.Errorf("prompt =\n%q\nwant\n%q", got, want)
	}
}

// TestMissingSessionCwdOptionsMatchTheReference pins session-cwd.ts
// getMissingSessionCwdOptions: the labels name the fallback directory, fork
// keeps the original, and the last choice is cancel.
func TestMissingSessionCwdOptionsMatchTheReference(t *testing.T) {
	issue := SessionCwdIssue{SessionCwd: "/old/dir", FallbackCwd: "/new/dir"}
	want := []MissingSessionCwdOption{
		{Label: "Continue in /new/dir (asks again next time)", Choice: MissingSessionCwdContinue},
		{Label: "Fork into /new/dir (new session ID, original kept)", Choice: MissingSessionCwdFork},
		{Label: "Cancel", Choice: MissingSessionCwdCancel},
	}
	got := MissingSessionCwdOptions(issue)
	if len(got) != len(want) {
		t.Fatalf("options = %+v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("option %d = %+v, want %+v", index, got[index], want[index])
		}
	}
}
