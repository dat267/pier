package interactive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

// pi v1.4.0 (4a080252), interactive-mode.ts showTrustSelector reads the
// trust store before opening the selector; failures cannot become "unknown".
func TestTrustSelectorReportsReadFailure(t *testing.T) {
	wiring, _, _, _ := newSelectorTestWiring(t)
	wiring.AgentDir = t.TempDir()
	wiring.SessionInfo = coding.NewSessionManager(t.TempDir(), &coding.SessionManagerOptions{Persist: new(false)})
	path := filepath.Join(wiring.AgentDir, "trust.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	var errorsShown []string
	wiring.ShowError = func(message string) { errorsShown = append(errorsShown, message) }
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("selector panicked instead of reporting read failure: %v", recovered)
		}
	}()
	wiring.ShowTrustSelector()
	if len(errorsShown) != 1 || !strings.Contains(errorsShown[0], path) {
		t.Fatalf("errors = %v; want trust-store read diagnostic", errorsShown)
	}
	if wiring.Slot.HasActiveSelector() {
		t.Fatal("opened trust selector despite a failed read")
	}
}

// pi v1.4.0 interactive-mode.ts maybeSaveImplicitProjectTrustAfterReload
// catches trust-store failures, reports them, and never overwrites the store.
func TestImplicitTrustReportsReadFailure(t *testing.T) {
	wiring, _, _ := newHelpersTestWiring(t)
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".pi"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".pi", "settings.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	wiring.SessionInfo = coding.NewSessionManager(project, &coding.SessionManagerOptions{Persist: new(false)})
	wiring.Settings = coding.NewInMemorySettingsManager(nil, coding.SettingsManagerCreateOptions{ProjectTrusted: new(true)})
	path := filepath.Join(wiring.AgentDir, "trust.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	var errorsShown []string
	wiring.ShowError = func(message string) { errorsShown = append(errorsShown, message) }
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("implicit trust panicked instead of reporting read failure: %v", recovered)
		}
	}()
	if wiring.MaybeSaveImplicitProjectTrustAfterReload(project) {
		t.Fatal("saved implicit trust after a failed read")
	}
	if len(errorsShown) != 1 || !strings.Contains(errorsShown[0], path) {
		t.Fatalf("errors = %v; want trust-store read diagnostic", errorsShown)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "{" {
		t.Fatalf("failed read altered store: %q %v", raw, err)
	}
}

// Upstream showTrustSelector cannot report success if setMany's locked reread
// fails after the selector opened (interactive-mode.ts, pi v1.4.0).
func TestTrustSelectorReportsFailureAfterOpening(t *testing.T) {
	wiring, _, _, _ := newSelectorTestWiring(t)
	wiring.AgentDir = t.TempDir()
	project := t.TempDir()
	wiring.SessionInfo = coding.NewSessionManager(project, &coding.SessionManagerOptions{Persist: new(false)})
	var errorsShown, statuses []string
	wiring.ShowError = func(message string) { errorsShown = append(errorsShown, message) }
	wiring.ShowStatus = func(message string) { statuses = append(statuses, message) }
	wiring.ShowTrustSelector()
	selector, ok := wiring.Slot.ActiveSelectorComponent().(*TrustSelectorComponent)
	if !ok {
		t.Fatal("trust selector did not open")
	}
	path := filepath.Join(wiring.AgentDir, "trust.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	selector.Select(TrustSelection{Trusted: true, Updates: []coding.ProjectTrustUpdate{{Path: project, Decision: new(true)}}})
	if len(errorsShown) != 1 || !strings.Contains(errorsShown[0], path) {
		t.Errorf("errors = %v; want trust-store diagnostic", errorsShown)
	}
	if len(statuses) != 0 || !wiring.Slot.HasActiveSelector() {
		t.Errorf("failed save reported success or closed selector: %v", statuses)
	}
}
