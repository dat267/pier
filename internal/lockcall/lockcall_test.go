package lockcall

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture marks each call that must be reported with a "want" comment.
func TestFindReportsCallsThroughFuncValuesUnderALock(t *testing.T) {
	pkgs, err := Load(filepath.Join("testdata", "fixture"), ".")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, finding := range Find(pkgs) {
		got = append(got, fmt.Sprintf("%d %s %s", finding.Pos.Line, finding.Callee, finding.Lock))
	}
	want := []string{
		"24 o.listener o.mu",
		"25 callback o.mu",
		"26 o.listeners[1] o.mu",
		"27 hook o.mu",
		"35 o.listener o.rw",
		"45 o.listener o.mu",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// allowed are the reviewed calls made under a lock on purpose, keyed by
// "file function callee". Each one's lock exists to cover the call.
var allowed = map[string]string{
	"ai/federation.go (*FederationTokenCache).Token now":                      "an injectable clock",
	"ai/models_runtime.go (*Models).publishProviderModels publication.Update": "the per-provider publication lock orders each publish, Update included",
	"coding/authstorage.go (*InMemoryAuthStorageBackend).WithLock fn":         "WithLock's contract is to run fn under the lock",
	"coding/bashexecutor.go (*killGate).runKill kill":                         "the gate serializes the kill with the wait's completion",
	"coding/mutationqueue.go WithFileMutationQueue fn":                        "the queue's contract is to run fn under the file's lock",
	"coding/settingsmanager.go (*InMemorySettingsStorage).WithLock fn":        "WithLock's contract is to run fn under the lock",
	"coding/timings.go ResetTimings timingNow":                                "an injectable clock",
	"mcp/oauth/provider.go (*McpOAuthProvider).update mutate":                 "the write lock makes the read-modify-write atomic",
}

// TestNoCallbacksUnderLocks keeps new code from calling a func value with a
// mutex held (AGENTS.md: snapshot callbacks under the lock, deliver after
// unlocking). Fix a new finding that way; add it to allowed only when the lock
// exists to cover the call.
func TestNoCallbacksUnderLocks(t *testing.T) {
	if testing.Short() {
		t.Skip("type-checks the whole module")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := Load(root, "./...")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, finding := range Find(pkgs) {
		rel, err := filepath.Rel(root, finding.Pos.Filename)
		if err != nil {
			t.Fatal(err)
		}
		key := filepath.ToSlash(rel) + " " + finding.Func + " " + finding.Callee
		seen[key] = true
		if _, ok := allowed[key]; !ok {
			t.Errorf("%s:%d: %s calls %s while holding %s; snapshot it under the lock and call it after unlocking",
				filepath.ToSlash(rel), finding.Pos.Line, finding.Func, finding.Callee, finding.Lock)
		}
	}
	for key := range allowed {
		if !seen[key] {
			t.Errorf("allowed entry %q no longer matches a call; remove it", key)
		}
	}
}
