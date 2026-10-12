package coding

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Round 117 tests: git metadata discovery, branch resolution, and deprecation
// warnings.

// footer-data-provider.ts reads HEAD as UTF-8 text before parsing the branch.
func TestFooterDataProviderReplacesMalformedUTF8InHEAD(t *testing.T) {
	repo := t.TempDir()
	gitDir := filepath.Join(repo, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), append([]byte("ref: refs/heads/feature-"), 0xff, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	provider := NewFooterDataProvider(repo, FooterDataProviderOptions{DisableWatch: true})
	branch, ok := provider.GetGitBranch()
	if !ok || branch != "feature-\uFFFD" {
		t.Fatalf("branch = %q, %v; want replacement-decoded branch", branch, ok)
	}
}

// footer-data-provider.ts requests UTF-8 decoding for git's branch output.
func TestResolveGitBranchReplacesMalformedUTF8(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX git test stub")
	}
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "git")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nprintf 'feature-\\377\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got := ResolveGitBranch(t.TempDir()); got != "feature-\uFFFD" {
		t.Fatalf("branch = %q; want replacement-decoded name", got)
	}
}

func TestResolveGitBranch(t *testing.T) {
	if _, err := execLookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	run := func(args ...string) {
		command := exec.Command("git", args...)
		command.Dir = repo
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	run("init", "-q", "-b", "main", ".")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")

	if branch := ResolveGitBranch(repo); branch != "main" {
		t.Fatalf("branch = %q", branch)
	}

	// Detached HEAD resolves to empty.
	run("checkout", "-q", "--detach", "HEAD")
	if branch := ResolveGitBranch(repo); branch != "" {
		t.Fatalf("branch = %q", branch)
	}

	// A non-repository directory resolves to empty.
	if branch := ResolveGitBranch(t.TempDir()); branch != "" {
		t.Fatalf("branch = %q", branch)
	}
}

func TestWarnDeprecation(t *testing.T) {
	WarnDeprecation("old thing")
	WarnDeprecation("old thing") // deduplicated
	WarnDeprecation("other thing")
	warnings := TakeDeprecationWarnings()
	if strings.Join(warnings, ",") != "old thing,other thing" {
		t.Fatalf("warnings = %v", warnings)
	}
	// After the take, the state is cleared and warnings emit again.
	WarnDeprecation("old thing")
	if warnings := TakeDeprecationWarnings(); len(warnings) != 1 || warnings[0] != "old thing" {
		t.Fatalf("warnings = %v", warnings)
	} else {
		TakeDeprecationWarnings()
	}
}

// execLookPath reports whether a command is on the PATH.
func execLookPath(name string) (string, error) { return exec.LookPath(name) }
