package uiblock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Go-only D143 guard for pi v1.4.0's single-writer TUI contract. os.Hostname
// makes OS calls (and can read /proc on Linux); it is not a cheap environment
// lookup and must be reported at a UI root.
func TestAnalysisReportsHostnameSyscall(t *testing.T) {
	dir := analysisFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "hostname.go"), []byte(`package fixture
import "os"
type HostnameSurface struct{}
func (HostnameSurface) Render() { os.Hostname(); os.Getenv("TERM") }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := Find(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		if finding.Kind == Syscall && strings.Contains(strings.Join(finding.Chain, " "), "HostnameSurface") {
			return
		}
	}
	t.Fatalf("os.Hostname was not reported as a syscall at a UI root: %+v", findings)
}
