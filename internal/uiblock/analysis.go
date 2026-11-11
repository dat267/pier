// Package uiblock finds code that can block the UI goroutine.
//
// The invariant is that nothing expensive or blocking runs on the main event
// loop: the loop applies cheap, prepared state and paints. This package
// statically approximates "runs on the UI goroutine" and flags everything
// reachable that can block: syscalls and file I/O, network, process execution,
// blocking channel operations, lock acquisitions, sleeps, and JSON
// marshal/decode (CPU-bound — the class the markdown and fenced-code freezes
// belonged to).
//
// Method. Roots are the UI-loop entry points: the loop and drain functions,
// every pier implementation of HandleEvent / HandleTerminalInput / RenderNow /
// Render / Invalidate / HandleSignal, the beat's materializers, every closure
// marshaled onto the loop via UI.Post / runOnUI, and every closure handed to a
// helper's post-back callback (loopCallbackParams), which is how the off-loop
// framework delivers finished work. From each root the traversal follows static
// call edges (direct calls to named functions and methods) plus interface-method
// invocations resolved to pier implementations
// by method name. Dynamic dispatch through func values is deliberately not
// followed: a func-typed field can hold any closure of its signature, and
// signature-based expansion (CHA) produces impossible chains; the surfaces the
// loop actually calls through func fields are rooted explicitly instead.
// `go` statements are not followed during that traversal — they run on other
// goroutines — but a separate advisory pass (FindGoroutines) lists a bare
// goroutine whose body blocks, to point it at internal/offloop.
package uiblock

import (
	"fmt"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// Kind classifies why a site can block.
type Kind int

const (
	Syscall Kind = iota
	Net
	Exec
	Channel
	Lock
	Sleep
	CPU
	Goroutine
)

func (k Kind) String() string {
	return [...]string{"syscall/io", "net", "exec", "channel-op", "lock", "sleep", "cpu-bound", "goroutine"}[k]
}

// Finding is one blocking site reachable from the UI goroutine, with the call
// chain (root first) that reaches it.
type Finding struct {
	Kind  Kind
	Pos   string
	Chain []string
}

var pkgBlockers = map[string]Kind{
	"os":        Syscall,
	"os/exec":   Exec,
	"net":       Net,
	"net/http":  Net,
	"syscall":   Syscall,
	"os/user":   Syscall,
	"io/ioutil": Syscall,
}

// cheapOS are os functions that never touch the kernel.
var cheapOS = map[string]bool{
	// UserHomeDir reads $HOME (os/file.go), so it makes no syscall.
	"os.UserHomeDir": true,
	"os.JoinPath":    true, "os.PathSeparator": true, "os.PathListSeparator": true,
	"os.Getenv": true, "os.Setenv": true, "os.Unsetenv": true, "os.Clearenv": true,
	"os.Expand": true, "os.Getpagesize": true,
}

var namedBlockers = map[string]Kind{
	"sync.(*Mutex).Lock":              Lock,
	"sync.(*Mutex).TryLock":           Lock,
	"sync.(*RWMutex).Lock":            Lock,
	"sync.(*RWMutex).RLock":           Lock,
	"sync.(*WaitGroup).Wait":          Lock,
	"sync.(*Once).Do":                 Lock,
	"sync.(*Cond).Wait":               Lock,
	"time.Sleep":                      Sleep,
	"runtime.GC":                      CPU,
	"runtime/debug.FreeOSMemory":      CPU,
	"encoding/json.Marshal":           CPU,
	"encoding/json.Unmarshal":         CPU,
	"encoding/json.(*Encoder).Encode": CPU,
	"encoding/json.(*Decoder).Decode": CPU,
	"encoding/json.MarshalIndent":     CPU,
	"path/filepath.Walk":              Syscall,
	"path/filepath.WalkDir":           Syscall,
	"path/filepath.Glob":              Syscall,
	"io.Copy":                         Syscall,
	"io.ReadAll":                      Syscall,
}

func blockingCallee(fn *ssa.Function) (Kind, bool) {
	rel := fn.RelString(nil)
	if k, ok := namedBlockers[rel]; ok {
		return k, true
	}
	// Resolve the callee's package from its object, not from RelString: a
	// method's name is "(*os.File).Write", so splitting on the first dot yields
	// "(*os" and every method of a blocking package was silently skipped.
	if obj := fn.Object(); obj != nil {
		if pkg := obj.Pkg(); pkg != nil {
			if k, ok := pkgBlockers[pkg.Path()]; ok && !cheapOS[rel] {
				return k, true
			}
		}
	}
	return 0, false
}

var pierRootRE = regexp.MustCompile(`^\(\*?github\.com/dat267/pier/[^()]+\)\.(HandleEvent|HandleTerminalInput|RenderNow|Render|Invalidate|HandleSignal)$`)

var pierNamedRoots = []string{
	"(*github.com/dat267/pier/coding/interactive.RunWiring).Run",
	"(*github.com/dat267/pier/coding/interactive.RunWiring).drainReadyEvents",
	"(*github.com/dat267/pier/coding/interactive.RunWiring).renderUI",
	"(*github.com/dat267/pier/coding/interactive.TranscriptRenderer).MaterializeDeferred",
	"(*github.com/dat267/pier/coding/interactive.QueueController).MaterializeThinkingChunk",
}

// module is the loaded SSA world plus the UI-loop roots and the method index
// that the traversals share.
type module struct {
	prog        *ssa.Program
	all         map[*ssa.Function]bool
	roots       map[*ssa.Function]string
	pierMethods map[string][]*ssa.Function
}

// moduleCache shares one loaded SSA world per directory. The package's three
// tests each want it, and building it is the dominant cost of the test suite
// (a full module load under -race); the module is read-only after loading, so
// sharing it is safe.
var moduleCache sync.Map // dir -> *moduleLoad

type moduleLoad struct {
	once   sync.Once
	module *module
	err    error
}

// cachedModule returns the shared module for dir, loading it once.
func cachedModule(dir string) (*module, error) {
	value, _ := moduleCache.LoadOrStore(dir, &moduleLoad{})
	load := value.(*moduleLoad)
	load.once.Do(func() { load.module, load.err = loadModule(dir) })
	return load.module, load.err
}

// loadModule builds the SSA program and computes the UI-loop roots.
func loadModule(dir string) (*module, error) {
	// Every repository package is an initial package. Keep external type and
	// function declarations for call-site classification, but do not parse or
	// build external bodies: traversal only follows isPier callees.
	cfg := &packages.Config{Mode: packages.LoadSyntax, Dir: dir}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, err
	}
	if packages.PrintErrors(pkgs) > 0 {
		return nil, fmt.Errorf("type errors during load")
	}
	prog, _ := ssautil.Packages(pkgs, ssa.BuilderMode(0))
	prog.Build()
	all := ssautil.AllFunctions(prog)

	// Pier functions indexed by trailing method name, for interface-method
	// resolution: an invoke of method M resolves to every pier function whose
	// name ends in ").M". Over-approximates within pier only.
	pierMethods := map[string][]*ssa.Function{}
	for fn := range all {
		if fn.Synthetic != "" || fn.Blocks == nil {
			continue
		}
		rel := fn.RelString(nil)
		if i := strings.LastIndex(rel, ")."); i >= 0 {
			name := rel[i+2:]
			pierMethods[name] = append(pierMethods[name], fn)
		}
	}

	roots := map[*ssa.Function]string{}
	for _, name := range pierNamedRoots {
		for fn := range all {
			if fn.RelString(nil) == name {
				roots[fn] = name
				break
			}
		}
	}
	for fn := range all {
		rel := fn.RelString(nil)
		isRoot := pierRootRE.MatchString(rel)
		if fn.Synthetic != "" && !isRoot {
			continue // wrappers matched by the regex forward to the real body
		}
		if isRoot {
			roots[fn] = rel
		}
	}
	// Closures stored into wiring-struct func fields are loop code by the same
	// marshaling discipline: the loop or loop-driven UI invokes them directly
	// (OnBeat, settings callbacks, key handlers). Their bodies are rooted here
	// because func-value calls are not followed during traversal.
	wiringClosures(prog, all, roots)
	loopCallbackClosures(all, roots)

	// Closures marshaled onto the loop via Post/runOnUI run on the UI goroutine.
	for fn := range all {
		if fn.Synthetic != "" || fn.Blocks == nil {
			continue
		}
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				call, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				callee := call.Common().StaticCallee()
				if callee == nil {
					continue
				}
				rel := callee.RelString(nil)
				if !strings.HasSuffix(rel, ".Post") && !strings.Contains(rel, "runOnUI") {
					continue
				}
				for _, arg := range call.Common().Args {
					switch v := arg.(type) {
					case *ssa.Function:
						if v.Synthetic == "" {
							roots[v] = "via " + rel
						}
					case *ssa.MakeClosure:
						if f, ok := v.Fn.(*ssa.Function); ok && f.Synthetic == "" {
							roots[f] = "via " + rel
						}
					}
				}
			}
		}
	}

	return &module{prog: prog, all: all, roots: roots, pierMethods: pierMethods}, nil
}

// rootSeeds returns the loop roots in a fixed order. The roots live in a map, so
// iterating it directly would let a function reachable from two roots report
// whichever chain the runtime happened to visit first, which makes findings and
// their allowlist keys unstable between runs.
func (m *module) rootSeeds() []rootSeed {
	seeds := make([]rootSeed, 0, len(m.roots))
	for fn, label := range m.roots {
		seeds = append(seeds, rootSeed{fn: fn, label: label})
	}
	sort.Slice(seeds, func(i, j int) bool {
		if seeds[i].label != seeds[j].label {
			return seeds[i].label < seeds[j].label
		}
		return seeds[i].fn.RelString(nil) < seeds[j].fn.RelString(nil)
	})
	return seeds
}

type rootSeed struct {
	fn    *ssa.Function
	label string
}

// Find runs the analysis over the module rooted at dir.
func Find(dir string) ([]Finding, error) {
	m, err := cachedModule(dir)
	if err != nil {
		return nil, err
	}
	prog, pierMethods := m.prog, m.pierMethods

	type node struct {
		fn    *ssa.Function
		chain []string
	}
	visited := map[*ssa.Function][]string{}
	queue := []node{}
	for _, seed := range m.rootSeeds() {
		visited[seed.fn] = []string{seed.label}
		queue = append(queue, node{seed.fn, []string{seed.label}})
	}
	var findings []Finding
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if n.fn.Blocks == nil {
			continue
		}
		for _, b := range n.fn.Blocks {
			for _, instr := range b.Instrs {
				switch ins := instr.(type) {
				case *ssa.Send:
					findings = append(findings, Finding{Channel, pos(prog, ins), n.chain})
				case *ssa.UnOp:
					if ins.Op == token.ARROW && !isLoopIdle(n.fn) {
						findings = append(findings, Finding{Channel, pos(prog, ins), n.chain})
					}
				case *ssa.Select:
					if ins.Blocking && !isLoopIdle(n.fn) {
						findings = append(findings, Finding{Channel, pos(prog, ins), n.chain})
					}
				case ssa.CallInstruction:
					com := ins.Common()
					callee := com.StaticCallee()
					if callee != nil {
						if k, ok := blockingCallee(callee); ok {
							findings = append(findings, Finding{k, pos(prog, ins), n.chain})
						}
						if _, isGo := ins.(*ssa.Go); isGo {
							continue
						}
						if !isPier(callee) {
							continue
						}
						if _, seen := visited[callee]; !seen {
							chain := append(append([]string{}, n.chain...), callee.RelString(nil))
							visited[callee] = chain
							queue = append(queue, node{callee, chain})
						}
						continue
					}
					// Dynamic dispatch. Interface-method invocations resolve to
					// pier implementations by method name; other dynamic calls
					// (func values) are not followed.
					if com.IsInvoke() && com.Method != nil {
						name := com.Method.Name()
						for _, impl := range pierMethods[name] {
							if !isPier(impl) {
								continue
							}
							// coding/rpcclient is the client side of the RPC
							// protocol: external processes drive a pier server
							// through it, and the app's own loop never wires it, so
							// its methods are not loop code. Without this, every
							// interface the app calls resolves to the client's
							// implementation as well and reports its I/O.
							if strings.HasPrefix(impl.RelString(nil), "(*github.com/dat267/pier/coding/rpcclient.") {
								continue
							}
							if _, seen := visited[impl]; !seen {
								chain := append(append([]string{}, n.chain...),
									impl.RelString(nil)+" (interface "+name+")")
								visited[impl] = chain
								queue = append(queue, node{impl, chain})
							}
						}
					}
				}
			}
		}
	}

	seen := map[string]Finding{}
	for _, f := range findings {
		key := f.Kind.String() + "|" + f.Pos
		if prev, ok := seen[key]; !ok || len(prev.Chain) < len(f.Chain) {
			seen[key] = f
		}
	}
	list := make([]Finding, 0, len(seen))
	for _, f := range seen {
		list = append(list, f)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Kind != list[j].Kind {
			return list[i].Kind < list[j].Kind
		}
		return list[i].Pos < list[j].Pos
	})
	return list, nil
}

func pos(prog *ssa.Program, ins ssa.Instruction) string {
	p := prog.Fset.Position(ins.Pos())
	if !p.IsValid() {
		return "(no pos)"
	}
	return p.String()
}

// wiringClosures roots every closure stored into a func-typed field of a
// pier wiring struct (types named *Wiring in the module).
func wiringClosures(prog *ssa.Program, all map[*ssa.Function]bool, roots map[*ssa.Function]string) {
	isWiring := func(t types.Type) bool {
		named, ok := t.(*types.Named)
		if !ok {
			return false
		}
		obj := named.Obj()
		return obj != nil && strings.HasSuffix(obj.Name(), "Wiring") &&
			strings.Contains(obj.Pkg().Path(), "github.com/dat267/pier")
	}
	for fn := range all {
		if fn.Synthetic != "" || fn.Blocks == nil || !isPier(fn) {
			continue
		}
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				store, ok := instr.(*ssa.Store)
				if !ok {
					continue
				}
				field, ok := store.Addr.(*ssa.FieldAddr)
				if !ok {
					continue
				}
				// The field's type must be a signature; the struct owning it
				// is found from the allocation the field address is based on.
				if _, ok := typeUnderPointer(field.Type()).(*types.Signature); !ok {
					continue
				}
				alloc, ok := field.X.(*ssa.Alloc)
				if !ok || !isWiring(typeUnderPointer(alloc.Type())) {
					continue
				}
				switch v := store.Val.(type) {
				case *ssa.MakeClosure:
					if f, ok := v.Fn.(*ssa.Function); ok && f.Synthetic == "" {
						roots[f] = "wiring field " + fieldName(field)
					}
				case *ssa.Function:
					if v.Synthetic == "" {
						roots[v] = "wiring field " + fieldName(field)
					}
				}
			}
		}
	}
}

// loopCallbackParams lists the helpers whose named callback parameter runs back
// on the owner loop. The traversal cannot follow a func value, so a closure
// handed to such a parameter is rooted directly: it is what the app applies
// after the off-loop work finishes, and a blocking call in it blocks the loop
// exactly like a direct root. runSessionFileWork passes its done callback to the
// post function it is given, which is the app's postToOwnerLoop.
var loopCallbackParams = []struct {
	function  string
	parameter string
}{
	{"runSessionFileWork", "done"},
}

// loopCallbackClosures roots closures handed to a post-back callback. The index
// comes from the callee's own parameter names, so a generic instantiation's
// leading dictionary parameter cannot shift it.
func loopCallbackClosures(all map[*ssa.Function]bool, roots map[*ssa.Function]string) {
	for fn := range all {
		if fn.Blocks == nil {
			continue
		}
		for _, block := range fn.Blocks {
			for _, instr := range block.Instrs {
				call, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				common := call.Common()
				callee := common.StaticCallee()
				if callee == nil {
					continue
				}
				name := callee.RelString(nil)
				for _, entry := range loopCallbackParams {
					if !strings.Contains(name, entry.function) {
						continue
					}
					index := -1
					for position, param := range callee.Params {
						if param.Name() == entry.parameter {
							index = position
							break
						}
					}
					if index < 0 || index >= len(common.Args) {
						continue
					}
					// A closure with no captures is passed as a bare function value.
					switch value := common.Args[index].(type) {
					case *ssa.Function:
						if value.Synthetic == "" {
							roots[value] = "via " + name + "." + entry.parameter
						}
					case *ssa.MakeClosure:
						if f, ok := value.Fn.(*ssa.Function); ok && f.Synthetic == "" {
							roots[f] = "via " + name + "." + entry.parameter
						}
					}
				}
			}
		}
	}
}

func fieldName(field *ssa.FieldAddr) string {
	structType := typeUnderPointer(field.X.Type())
	if named, ok := structType.(*types.Named); ok {
		if st, ok := named.Underlying().(*types.Struct); ok && field.Field < st.NumFields() {
			return st.Field(field.Field).Name()
		}
	}
	return "?"
}

func typeUnderPointer(t types.Type) types.Type {
	if p, ok := t.(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

// isPier reports whether the function belongs to this module. Traversal and
// flagging stay inside pier: a blocking stdlib callee is flagged at the pier
// call site, and stdlib-internal blocking is reached only through entry points
// the denylist already names.
func isPier(fn *ssa.Function) bool {
	rel := fn.RelString(nil)
	// Package-level functions are "github.com/dat267/pier/...pkg.Func"; methods
	// are "(github.com/dat267/pier/...Type).Method" (receiver first).
	// Almost every type here is used through a pointer, so the pointer form is
	// the common one: "(*github.com/dat267/pier/...Type).Method". Missing it made
	// the walk skip those methods, which hid a real stall (A493).
	return strings.HasPrefix(rel, "github.com/dat267/pier/") ||
		strings.HasPrefix(rel, "(github.com/dat267/pier/") ||
		strings.HasPrefix(rel, "(*github.com/dat267/pier/")
}

// isLoopIdle marks the loop's own select: those receives are the idle wait.
func isLoopIdle(fn *ssa.Function) bool {
	rel := fn.RelString(nil)
	return strings.Contains(rel, "(*RunWiring).Run") ||
		strings.Contains(rel, "(*RunWiring).drainReadyEvents")
}
