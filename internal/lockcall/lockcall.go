// Package lockcall finds calls through func values made while a sync mutex is
// held.
//
// A func-typed field, parameter, variable or map entry can hold any caller's
// code. Calling one under a retained lock runs that code with the lock held: if
// it re-enters the owner (a listener that reads state back, a callback that
// unsubscribes) it deadlocks, and if it blocks it stalls every other user of
// the lock. AGENTS.md says to snapshot callbacks under the lock and deliver
// after unlocking; this check enforces it.
//
// Method. Each function body is walked statement by statement. `m.Lock()` or
// `m.RLock()` on a sync.Mutex or sync.RWMutex marks m held until the matching
// unlock in the same block (a deferred unlock holds it to the end of the
// function). Nested blocks inherit the held set, and an unlock inside a nested
// block (the early-return pattern) does not release the outer one. While any
// lock is held, a call whose callee is a value rather than a declared function
// or method is reported. Function literals are not entered: they run later,
// usually after the unlock. Locks taken by a caller ("caller holds the lock"
// helpers) are not tracked, so the check is a floor, not a proof.
package lockcall

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Finding is one call through a func value under a held lock.
type Finding struct {
	Pos    token.Position
	Func   string // enclosing function
	Callee string // the called expression
	Lock   string // a held lock
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s calls %s while holding %s", f.Pos.Filename, f.Pos.Line, f.Func, f.Callee, f.Lock)
}

// Load type-checks the packages matching patterns (non-test files).
func Load(dir string, patterns ...string) ([]*packages.Package, error) {
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:  dir,
	}
	pkgs, err := packages.Load(config, patterns...)
	if err != nil {
		return nil, err
	}
	var errs []string
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		for _, e := range pkg.Errors {
			errs = append(errs, e.Error())
		}
	})
	if len(errs) > 0 {
		return nil, fmt.Errorf("load: %s", strings.Join(errs, "; "))
	}
	return pkgs, nil
}

// Find reports every call through a func value made under a held lock.
func Find(pkgs []*packages.Package) []Finding {
	var out []Finding
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				w := &walker{pkg: pkg, fn: funcName(fn)}
				w.block(fn.Body.List, nil)
				out = append(out, w.found...)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pos.Filename != out[j].Pos.Filename {
			return out[i].Pos.Filename < out[j].Pos.Filename
		}
		return out[i].Pos.Line < out[j].Pos.Line
	})
	return out
}

func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	recv := types.ExprString(fn.Recv.List[0].Type)
	return "(" + recv + ")." + fn.Name.Name
}

type walker struct {
	pkg   *packages.Package
	fn    string
	found []Finding
}

// block walks statements in order with the locks held on entry; it returns
// nothing because a nested block's unlocks never release its parent's locks.
func (w *walker) block(stmts []ast.Stmt, inherited []string) {
	held := append([]string(nil), inherited...)
	for _, stmt := range stmts {
		if lock, op := w.lockOp(stmt); op != "" {
			switch op {
			case "lock":
				held = append(held, lock)
			case "unlock":
				held = remove(held, lock)
			}
			continue
		}
		w.stmt(stmt, held)
	}
}

func remove(held []string, lock string) []string {
	for i := len(held) - 1; i >= 0; i-- {
		if held[i] == lock {
			return append(held[:i:i], held[i+1:]...)
		}
	}
	return held
}

// lockOp recognizes `m.Lock()`, `m.RLock()`, `m.Unlock()`, `m.RUnlock()` and a
// deferred unlock (which keeps the lock held, so it is reported as no-op "defer").
func (w *walker) lockOp(stmt ast.Stmt) (string, string) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		call, ok := s.X.(*ast.CallExpr)
		if !ok {
			return "", ""
		}
		name, lock := w.mutexMethod(call)
		switch name {
		case "Lock", "RLock":
			return lock, "lock"
		case "Unlock", "RUnlock":
			return lock, "unlock"
		}
	case *ast.DeferStmt:
		if name, lock := w.mutexMethod(s.Call); name == "Unlock" || name == "RUnlock" {
			return lock, "defer"
		}
	}
	return "", ""
}

// mutexMethod returns the method name and receiver text of a call to a
// sync.Mutex or sync.RWMutex method.
func (w *walker) mutexMethod(call *ast.CallExpr) (string, string) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", ""
	}
	selection := w.pkg.TypesInfo.Selections[sel]
	if selection == nil || selection.Kind() != types.MethodVal {
		return "", ""
	}
	method := selection.Obj()
	if method.Pkg() == nil || method.Pkg().Path() != "sync" {
		return "", ""
	}
	recv := selection.Recv()
	if pointer, ok := recv.(*types.Pointer); ok {
		recv = pointer.Elem()
	}
	named, ok := recv.(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "sync" {
		// An embedded mutex promotes its methods; the receiver is then the
		// embedding struct, which still names the lock.
		if !isMutexMethod(method) {
			return "", ""
		}
	}
	return sel.Sel.Name, types.ExprString(sel.X)
}

func isMutexMethod(method types.Object) bool {
	fn, ok := method.(*types.Func)
	if !ok {
		return false
	}
	recv := fn.Type().(*types.Signature).Recv()
	if recv == nil {
		return false
	}
	t := recv.Type()
	if pointer, ok := t.(*types.Pointer); ok {
		t = pointer.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	name := named.Obj().Name()
	return named.Obj().Pkg().Path() == "sync" && (name == "Mutex" || name == "RWMutex")
}

// stmt inspects one statement under held, descending into nested blocks with
// the current held set.
func (w *walker) stmt(stmt ast.Stmt, held []string) {
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		w.block(s.List, held)
	case *ast.IfStmt:
		if s.Init != nil {
			w.stmt(s.Init, held)
		}
		w.expr(s.Cond, held)
		w.block(s.Body.List, held)
		if s.Else != nil {
			w.stmt(s.Else, held)
		}
	case *ast.ForStmt:
		if s.Init != nil {
			w.stmt(s.Init, held)
		}
		if s.Cond != nil {
			w.expr(s.Cond, held)
		}
		if s.Post != nil {
			w.stmt(s.Post, held)
		}
		w.block(s.Body.List, held)
	case *ast.RangeStmt:
		w.expr(s.X, held)
		w.block(s.Body.List, held)
	case *ast.SwitchStmt:
		if s.Init != nil {
			w.stmt(s.Init, held)
		}
		if s.Tag != nil {
			w.expr(s.Tag, held)
		}
		for _, clause := range s.Body.List {
			c := clause.(*ast.CaseClause)
			for _, e := range c.List {
				w.expr(e, held)
			}
			w.block(c.Body, held)
		}
	case *ast.TypeSwitchStmt:
		if s.Init != nil {
			w.stmt(s.Init, held)
		}
		w.stmt(s.Assign, held)
		for _, clause := range s.Body.List {
			w.block(clause.(*ast.CaseClause).Body, held)
		}
	case *ast.SelectStmt:
		for _, clause := range s.Body.List {
			c := clause.(*ast.CommClause)
			if c.Comm != nil {
				w.stmt(c.Comm, held)
			}
			w.block(c.Body, held)
		}
	case *ast.LabeledStmt:
		w.stmt(s.Stmt, held)
	case *ast.GoStmt:
		// The goroutine runs without the lock; only its arguments are
		// evaluated here.
		for _, arg := range s.Call.Args {
			w.expr(arg, held)
		}
	case *ast.DeferStmt:
		// A deferred call runs at return, when the lock may be released.
		for _, arg := range s.Call.Args {
			w.expr(arg, held)
		}
	default:
		ast.Inspect(stmt, func(node ast.Node) bool { return w.visit(node, held) })
	}
}

func (w *walker) expr(e ast.Expr, held []string) {
	ast.Inspect(e, func(node ast.Node) bool { return w.visit(node, held) })
}

func (w *walker) visit(node ast.Node, held []string) bool {
	switch n := node.(type) {
	case *ast.FuncLit:
		return false
	case *ast.CallExpr:
		if len(held) > 0 && w.dynamic(n) {
			w.found = append(w.found, Finding{
				Pos:    w.pkg.Fset.Position(n.Pos()),
				Func:   w.fn,
				Callee: types.ExprString(n.Fun),
				Lock:   held[len(held)-1],
			})
		}
	}
	return true
}

// dynamic reports a call whose callee is a func value: a variable, parameter,
// field, map or slice element, or call result. Calls of declared functions,
// methods (including interface methods, whose implementations are declared),
// builtins, conversions and immediately invoked literals are static.
func (w *walker) dynamic(call *ast.CallExpr) bool {
	info := w.pkg.TypesInfo
	tv, ok := info.Types[call.Fun]
	if ok && (tv.IsType() || tv.IsBuiltin()) {
		return false
	}
	if ok && isCancelFunc(tv.Type) {
		// A context cancel runs no caller code synchronously (AfterFunc
		// callbacks get their own goroutines).
		return false
	}
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.FuncLit:
		return false
	case *ast.Ident:
		_, isVar := info.Uses[fun].(*types.Var)
		return isVar
	case *ast.SelectorExpr:
		if selection := info.Selections[fun]; selection != nil {
			return selection.Kind() == types.FieldVal
		}
		_, isVar := info.Uses[fun.Sel].(*types.Var) // a package-level func variable
		return isVar
	case *ast.IndexExpr, *ast.IndexListExpr:
		// A generic instantiation names a function; an index into a map or
		// slice of funcs is a value.
		if tv, ok := info.Types[call.Fun]; ok {
			_, isSig := tv.Type.Underlying().(*types.Signature)
			if !isSig {
				return false
			}
		}
		if index, ok := fun.(*ast.IndexExpr); ok {
			if _, generic := info.Instances[identOf(index.X)]; generic {
				return false
			}
		}
		return true
	default:
		return true
	}
}

func identOf(e ast.Expr) *ast.Ident {
	switch x := e.(type) {
	case *ast.Ident:
		return x
	case *ast.SelectorExpr:
		return x.Sel
	}
	return nil
}

func isCancelFunc(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	name := named.Obj().Name()
	return named.Obj().Pkg().Path() == "context" && (name == "CancelFunc" || name == "CancelCauseFunc")
}
