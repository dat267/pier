package uiblock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// analysisFixture exercises the analyzer's public entry points without loading
// the application. Every blocking path has a distinct root so each case names
// the finding it means. Unreachable code must not be reported.
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
// A method of a blocking stdlib type: its name is "(*os.File).Write", so the
// package must come from the callee's object, not from splitting the name.
func (Surface) RenderFile(f *os.File) { f.Write([]byte("method")) }
// A pointer-receiver implementation of a loop entry point: documentation
// promises every implementation is a root, and Go UI types are pointers.
type PointerSurface struct{}
func (*PointerSurface) Invalidate() { os.Stat("pointer receiver root") }
func (Surface) Invalidate() { go helper.Background() }
// A pointer-receiver method is pier code, so the walk must descend into it: the
// descent guard spelled only the value-receiver form, which skipped nearly every
// method in this module (A493).
type MethodSurface struct{}
func (MethodSurface) HandleEvent() { (&MethodSurface{}).TouchFile() }
func (*MethodSurface) TouchFile() { os.Stat("method") }
// RenderFile runs from a value-receiver root, so the callee-classification case
// does not also depend on pointer-receiver roots.
type ValueHost struct{}
func (ValueHost) HandleEvent(f *os.File) { Surface{}.RenderFile(f) }
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
// Work finishes on a queue and posts its result back: the posted closure is what
// the loop applies, and the func value that delivers it is invisible to a static
// traversal, so the callback parameter is where the loop side has to be rooted.
func runSessionFileWork[T any](work func() T, done func(T)) { done(work()) }
func registerLoopCallback() {
 runSessionFileWork(func() int { return 1 }, func(int) { os.Remove("loop callback") })
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
		".Block":                    Syscall,
		"interface Work":            Syscall,
		"wiring field OnBeat":       Syscall,
		".Post":                     Syscall,
		".RenderNow":                Channel,
		".HandleSignal":             Sleep,
		".RenderFile":               Syscall,
		"PointerSurface":            Syscall,
		".MethodSurface).TouchFile": Syscall,
		"runSessionFileWork":        Syscall,
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

// TestRootSeedsAreOrdered guards the walk against map iteration order: a
// function reachable from two roots reports whichever chain the traversal visits
// first, so an unordered seed list would make a finding's chain head, and the
// allowlist key that names it, vary between runs.
func TestRootSeedsAreOrdered(t *testing.T) {
	m, err := cachedModule("../..")
	if err != nil {
		t.Fatalf("analysis failed: %v", err)
	}
	seeds := m.rootSeeds()
	if len(seeds) < 2 {
		t.Fatalf("only %d roots; the fixture is too small to order", len(seeds))
	}
	for i := 1; i < len(seeds); i++ {
		previous, current := seeds[i-1], seeds[i]
		before := previous.label < current.label ||
			(previous.label == current.label && previous.fn.RelString(nil) < current.fn.RelString(nil))
		if !before {
			t.Errorf("root %d (%q %s) does not sort before root %d (%q %s)",
				i-1, previous.label, previous.fn.RelString(nil), i, current.label, current.fn.RelString(nil))
		}
	}
}
