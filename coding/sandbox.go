package coding

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Sandbox rules, ported from deepseek-harness (packages/sandbox/sandbox/src/
// index.ts SandboxMode, with `danger-full-access` named `full-access` here) and
// from the user's sandbox extension (modes.ts, policy.ts, guard.ts). The three
// modes are file-effect policies for tool execution:
//
//	read-only       reads and executes everywhere, writes denied by the kernel
//	                (and every write/edit tool blocked in-process)
//	workspace-write writes under the workspace plus an allowlist (GOPATH,
//	                caches, temp, pi's agent dir)
//	full-access     no confinement
//
// Only Linux can enforce a mode with a kernel backend (Landlock). Where no
// backend exists workspace-write cannot be enforced, so the default falls back
// to read-only and the agent only writes after an explicit `/permission FA`.
//
// Divergence D177: upstream pi has no built-in sandbox (confinement is an
// out-of-scope extension); this is a port-local feature.

// SandboxMode is one file-effect policy.
type SandboxMode string

const (
	// SandboxModeReadOnly denies writes but allows reads and execution.
	SandboxModeReadOnly SandboxMode = "read-only"
	// SandboxModeWorkspaceWrite permits writes under the workspace and the
	// writable allowlist.
	SandboxModeWorkspaceWrite SandboxMode = "workspace-write"
	// SandboxModeFullAccess applies no confinement.
	SandboxModeFullAccess SandboxMode = "full-access"
)

// SandboxBackend names what can enforce a confined mode on this host.
type SandboxBackend string

const (
	// SandboxBackendLandlock is the Linux Landlock ruleset backend.
	SandboxBackendLandlock SandboxBackend = "landlock"
	// SandboxBackendNone means no enforcing backend is available.
	SandboxBackendNone SandboxBackend = "none"
)

// SandboxLauncherSubcommand is the hidden argv[1] that re-executes this binary
// as the Landlock launcher (the pure-Go replacement for the extension's
// compiled gate).
const SandboxLauncherSubcommand = "__sandbox-exec"

// sandboxDevNull is the one path read-only mode may write, because shells
// require the null sink.
const sandboxDevNull = "/dev/null"

var sandboxModeCodes = map[SandboxMode]string{
	SandboxModeReadOnly:       "RO",
	SandboxModeWorkspaceWrite: "WW",
	SandboxModeFullAccess:     "FA",
}

// sandboxModeOrder is the fixed presentation order (footer, completions); the
// parser iterates it too so a prefix match is deterministic.
var sandboxModeOrder = []SandboxMode{SandboxModeReadOnly, SandboxModeWorkspaceWrite, SandboxModeFullAccess}

// Code is the two-letter footer/command code for a mode.
func (m SandboxMode) Code() string { return sandboxModeCodes[m] }

// SandboxModeFromCode parses a `/permission` argument into a mode. It accepts
// the two-letter codes, the full mode names and an unambiguous prefix,
// case-insensitively.
func SandboxModeFromCode(arg string) (SandboxMode, bool) {
	value := strings.ToLower(strings.TrimSpace(arg))
	if value == "" {
		return "", false
	}
	for _, mode := range sandboxModeOrder {
		if strings.ToLower(mode.Code()) == value || string(mode) == value {
			return mode, true
		}
	}
	for _, mode := range sandboxModeOrder {
		if strings.HasPrefix(string(mode), value) {
			return mode, true
		}
	}
	return "", false
}

// SandboxModeCompletions are the `/permission` argument completions, derived
// from the same table as the parser and the footer.
func SandboxModeCompletions(prefix string) []SandboxModeCompletion {
	wanted := strings.ToUpper(strings.TrimSpace(prefix))
	var out []SandboxModeCompletion
	for _, mode := range sandboxModeOrder {
		code := mode.Code()
		if wanted != "" && !strings.HasPrefix(code, wanted) && !strings.HasPrefix(strings.ToUpper(string(mode)), wanted) {
			continue
		}
		out = append(out, SandboxModeCompletion{Value: code, Label: code, Description: SandboxModeDetail(mode, SandboxBackendNone)})
	}
	return out
}

// SandboxModeCompletion is one `/permission` argument completion.
type SandboxModeCompletion struct {
	Value       string
	Label       string
	Description string
}

// SandboxModeDetail is the human-facing detail for a mode (modes.ts DETAILS).
func SandboxModeDetail(mode SandboxMode, backend SandboxBackend) string {
	switch mode {
	case SandboxModeReadOnly:
		return "read-only (writes denied; read tools still work)"
	case SandboxModeWorkspaceWrite:
		if backend == SandboxBackendLandlock {
			return "kernel-enforced workspace (Landlock)"
		}
		return "kernel-enforced workspace"
	case SandboxModeFullAccess:
		return "unrestricted (all writes allowed)"
	}
	return string(mode)
}

// DefaultSandboxMode is the mode a fresh session starts in: workspace-write
// where a backend can enforce it, read-only otherwise (fail safe; full-access
// is an explicit toggle).
func DefaultSandboxMode(backend SandboxBackend) SandboxMode {
	if backend == SandboxBackendNone {
		return SandboxModeReadOnly
	}
	return SandboxModeWorkspaceWrite
}

// Sandbox is the session's filesystem policy: the active mode, the detected
// backend and the workspace the policy is scoped to. Tool executions read it,
// the `/permission` command writes it. A nil *Sandbox means "no policy" and
// reads as full-access, so a session built without one keeps its old behavior.
type Sandbox struct {
	mu        sync.RWMutex
	mode      SandboxMode
	backend   SandboxBackend
	workspace string
	home      string
}

// NewSandbox probes the backend and starts in the platform default mode.
func NewSandbox(workspace, home string) *Sandbox {
	backend := DetectSandboxBackend()
	return &Sandbox{
		mode:      DefaultSandboxMode(backend),
		backend:   backend,
		workspace: workspace,
		home:      home,
	}
}

// HomeForSandbox resolves the home directory the allowlist expands against.
func HomeForSandbox() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}

// Mode returns the active mode (full-access for a nil sandbox).
func (s *Sandbox) Mode() SandboxMode {
	if s == nil {
		return SandboxModeFullAccess
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode
}

// Backend returns the detected backend.
func (s *Sandbox) Backend() SandboxBackend {
	if s == nil {
		return SandboxBackendNone
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.backend
}

// Workspace returns the policy's workspace root.
func (s *Sandbox) Workspace() string {
	if s == nil {
		return ""
	}
	return s.workspace
}

// SetMode applies a requested mode. workspace-write on a host with no backend
// is unenforceable: it falls back to read-only and reports a warning. It
// returns the effective mode.
func (s *Sandbox) SetMode(requested SandboxMode) (SandboxMode, string) {
	if s == nil {
		return requested, ""
	}
	if requested == SandboxModeWorkspaceWrite && s.Backend() == SandboxBackendNone {
		s.mu.Lock()
		s.mode = SandboxModeReadOnly
		s.mu.Unlock()
		return SandboxModeReadOnly, "no kernel sandbox backend on this platform: workspace-write cannot be enforced, using read-only"
	}
	s.mu.Lock()
	s.mode = requested
	s.mu.Unlock()
	return requested, ""
}

// WritableRoots is the set of paths the active mode may write. read-only may
// only write the null sink; workspace-write adds the shared allowlist.
func (s *Sandbox) WritableRoots() []string {
	if s.Mode() == SandboxModeReadOnly {
		return []string{sandboxDevNull}
	}
	return SandboxWritableRoots(s.Workspace(), s.home)
}

// WrapArgv confines a command argv for the active mode. It returns the argv to
// run unchanged under full-access, the launcher-wrapped argv under a confined
// mode, or an error when the mode cannot be enforced (fail closed).
func (s *Sandbox) WrapArgv(argv []string) ([]string, error) {
	switch s.Mode() {
	case SandboxModeFullAccess:
		return argv, nil
	case SandboxModeReadOnly:
		if s.Backend() == SandboxBackendNone {
			return nil, fmt.Errorf("sandbox: read-only mode cannot confine commands on this platform (no kernel backend); /permission FA to run unconfined")
		}
		return sandboxConfinementArgv(s.WritableRoots(), argv)
	case SandboxModeWorkspaceWrite:
		if s.Backend() == SandboxBackendNone {
			return nil, fmt.Errorf("sandbox: workspace-write cannot be enforced on this platform (no kernel backend); /permission FA to run unconfined")
		}
		return sandboxConfinementArgv(s.WritableRoots(), argv)
	}
	return argv, nil
}

// CheckPath rejects an in-process file mutation that the active mode does not
// allow. The write/edit tools call it before touching the filesystem; bash is
// confined by the kernel instead.
func (s *Sandbox) CheckPath(target string) error {
	switch s.Mode() {
	case SandboxModeFullAccess:
		return nil
	case SandboxModeReadOnly:
		return fmt.Errorf("sandbox: read-only mode blocks writing %s; /permission WW to allow the workspace or /permission FA for full access", target)
	case SandboxModeWorkspaceWrite:
		if reason := InspectSandboxPath(target, s.Workspace(), SandboxWritableRoots(s.Workspace(), s.home)); reason != "" {
			return fmt.Errorf("%s", reason)
		}
		return nil
	}
	return nil
}

// SandboxWritableRoots is the shared writable allowlist (policy.ts
// posixAllowlist): the workspace, devices, temp, package caches, toolchains
// and pi's agent state. The kernel ruleset and the prompt note both derive from
// it, so enforcement and prose cannot drift.
func SandboxWritableRoots(workspace, home string) []string {
	roots := []string{
		workspace,
		"/tmp",
		"/dev",
		"/proc",
		"/sys",
		"/var/tmp",
	}
	if home != "" {
		roots = append(roots,
			home+"/go",      // GOPATH: module cache + go install binaries
			home+"/.rustup", // RUSTUP_HOME
			home+"/.cargo",  // CARGO_HOME
			home+"/.cache",
			home+"/.npm",
			home+"/.pi", // agent state: sessions, settings, skills, credentials
		)
	}
	seen := map[string]bool{}
	out := roots[:0]
	for _, root := range roots {
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		out = append(out, root)
	}
	sort.Strings(out)
	return out
}

// InspectSandboxPath returns a reason when target is outside the workspace and
// the allowlist, or "" when it is allowed. Symlinks are resolved, so a link
// inside the workspace cannot escape to an outside path (guard.ts inspectPath).
func InspectSandboxPath(target, workspace string, allowlist []string) string {
	resolved := realResolve(resolveSandboxArg(target, workspace))
	realWorkspace := cachedRealResolve(workspace)
	if resolved == realWorkspace || strings.HasPrefix(resolved, realWorkspace+string(filepath.Separator)) {
		return ""
	}
	for _, prefix := range allowlist {
		if resolved == prefix || strings.HasPrefix(resolved, prefix+string(filepath.Separator)) {
			return ""
		}
		real := cachedRealResolve(prefix)
		if resolved == real || strings.HasPrefix(resolved, real+string(filepath.Separator)) {
			return ""
		}
	}
	return "sandbox blocks " + resolved + ": outside the workspace"
}

func resolveSandboxArg(arg, base string) string {
	expanded := arg
	if expanded == "~" {
		expanded = HomeForSandbox()
	} else if strings.HasPrefix(expanded, "~/") {
		expanded = filepath.Join(HomeForSandbox(), expanded[2:])
	}
	if filepath.IsAbs(expanded) {
		return filepath.Clean(expanded)
	}
	return filepath.Join(base, expanded)
}

// realResolve resolves symlinks; a not-yet-existing target resolves its deepest
// existing ancestor and rejoins the missing leaves (guard.ts realResolve).
func realResolve(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	dir := filepath.Dir(path)
	leaves := []string{filepath.Base(path)}
	for i := 0; i < 40; i++ {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			for j := len(leaves) - 1; j >= 0; j-- {
				resolved = filepath.Join(resolved, leaves[j])
			}
			return resolved
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		leaves = append(leaves, filepath.Base(dir))
		dir = parent
	}
	return path
}

func cachedRealResolve(path string) string { return realResolve(path) }

// SandboxPromptNote renders the sandbox system-prompt section: the writable
// paths plus the mode's enforcement promise.
func SandboxPromptNote(mode SandboxMode, backend SandboxBackend, workspace, home string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Workspace filesystem policy (sandbox, mode: %s):\n", mode)
	if mode == SandboxModeFullAccess {
		b.WriteString("- The sandbox is DISABLED; all filesystem writes are unrestricted.")
		if backend == SandboxBackendNone {
			b.WriteString(" No kernel sandbox backend is available on this platform.")
		}
		return b.String()
	}
	roots := SandboxWritableRoots(workspace, home)
	rendered := make([]string, 0, len(roots))
	for _, root := range roots {
		rendered = append(rendered, renderSandboxPath(root, home))
	}
	fmt.Fprintf(&b, "- Writable: %s.\n", strings.Join(rendered, ", "))
	b.WriteString("- Every other directory is read-only. Reads are allowed everywhere.\n")
	b.WriteString("- Use /tmp for scratch files and test artifacts.\n")
	b.WriteString("- Deployments (chezmoi apply, extension installs) are run by the user in their own terminal, never by the agent.\n")
	switch mode {
	case SandboxModeReadOnly:
		if backend == SandboxBackendNone {
			b.WriteString("- Enforcement: no kernel backend is available, so bash, write and edit are blocked in-process.")
		} else {
			b.WriteString("- Enforcement: shell commands run under a kernel Landlock ruleset that denies writes (writes return Permission denied); write and edit targets are checked in-process.")
		}
	case SandboxModeWorkspaceWrite:
		b.WriteString("- Enforcement: shell commands run under a kernel Landlock ruleset (writes outside the list return Permission denied); write and edit targets are checked in-process with symlink resolution.")
	}
	return b.String()
}

func renderSandboxPath(entry, home string) string {
	if home != "" && strings.HasPrefix(entry, home) {
		return "~" + entry[len(home):]
	}
	return entry
}
