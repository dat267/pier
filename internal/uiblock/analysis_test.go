package uiblock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// analysisFixture exercises the analyzer's public entry points without loading
// the application. Every blocking path has a distinct root so root attribution
// cannot depend on traversal order. Unreachable code must not be reported.
func analysisFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module github.com/dat267/pier/fixture\n\ngo 1.27\n",
		"helper/helper.go": `package helper
import "os"
func Block() { os.Stat("direct") }
func Background() { os.ReadFile("background") }
func Unreachable() { os.Remove("unreachable") }
`,
		"surface.go": `package fixture
import (
 "os"
 "time"
 "github.com/dat267/pier/fixture/helper"
)
type Surface struct{}
func (Surface) Render() { helper.Block() }
type Worker struct{}
func (Worker) Work() { os.ReadFile("interface") }
type Job interface { Work() }
func invoke(job Job) { job.Work() }
func (Surface) HandleEvent() { invoke(Worker{}) }
func (Surface) RenderNow() { ch := make(chan int); ch <- 1 }
func (Surface) HandleSignal() { time.Sleep(time.Second) }
func (Surface) Invalidate() { go helper.Background() }
type FixtureWiring struct { OnBeat func() }
func makeWiring() *FixtureWiring {
 wiring := &FixtureWiring{}
 wiring.OnBeat = func() { os.Open("wiring") }
 return wiring
}
func (*Surface) Post(callback func()) {}
func registerPosted() {
 surface := &Surface{}
 surface.Post(func() { os.Mkdir("posted", 0700) })
}
`,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestAnalysisFindsBlockingPaths(t *testing.T) {
	dir := analysisFixture(t)
	findings, err := Find(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Kind{
		".Block":              Syscall,
		"interface Work":      Syscall,
		"wiring field OnBeat": Syscall,
		".Post":               Syscall,
		".RenderNow":          Channel,
		".HandleSignal":       Sleep,
	}
	if len(findings) != len(want) {
		t.Fatalf("findings = %+v, want %d distinct blocking paths", findings, len(want))
	}
	for path, kind := range want {
		found := false
		for _, finding := range findings {
			if finding.Kind == kind && strings.Contains(strings.Join(finding.Chain, " "), path) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %s path %q: %+v", kind, path, findings)
		}
	}
	goroutines, err := FindGoroutines(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(goroutines) != 1 || goroutines[0].Kind != Goroutine ||
		!strings.Contains(strings.Join(goroutines[0].Chain, " "), "go github.com/dat267/pier/fixture/helper.Background") {
		t.Fatalf("goroutine findings = %+v, want the imported background worker", goroutines)
	}
}

// External calls are classified at their repository call sites. Building their
// bodies wastes time and allocations without adding traversal coverage.
func TestModuleLeavesExternalFunctionBodiesUnbuilt(t *testing.T) {
	m, err := cachedModule(analysisFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	external := 0
	for _, pkg := range m.prog.AllPackages() {
		if strings.HasPrefix(pkg.Pkg.Path(), "github.com/dat267/pier/") {
			continue
		}
		for _, member := range pkg.Members {
			if function, ok := member.(*ssa.Function); ok {
				external++
				if len(function.Blocks) != 0 {
					t.Fatalf("external function body built: %s", function)
				}
			}
		}
	}
	if external == 0 {
		t.Fatal("fixture did not retain external function declarations")
	}
}
