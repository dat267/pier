//go:build linux

package coding

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Linux confinement: a Landlock ruleset applied to the running process, then
// exec of the command. This is the pure-Go replacement for the sandbox
// extension's compiled gate.c. The ruleset is inherited by the shell and every
// descendant, needs no root and no container, and fails closed: when Landlock
// is unavailable the command does not run.

const (
	landlockReadBits = unix.LANDLOCK_ACCESS_FS_EXECUTE |
		unix.LANDLOCK_ACCESS_FS_READ_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_DIR

	landlockWriteBits = unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
		unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_SYM
)

// DetectSandboxBackend probes the Landlock ABI (>= 1 means the kernel enforces
// a filesystem ruleset).
func DetectSandboxBackend() SandboxBackend {
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 || int(abi) < 1 {
		return SandboxBackendNone
	}
	return SandboxBackendLandlock
}

// sandboxConfinementArgv re-executes this binary as the launcher, then the
// command. The launcher applies the ruleset before exec, because Landlock
// restricts the calling process irrevocably and Go has no fork hook.
func sandboxConfinementArgv(allowlist []string, command []string) ([]string, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("sandbox: cannot resolve the executable: %w", err)
	}
	argv := make([]string, 0, len(allowlist)*2+len(command)+3)
	argv = append(argv, self, SandboxLauncherSubcommand)
	for _, path := range allowlist {
		argv = append(argv, "--allow", path)
	}
	argv = append(argv, "--")
	argv = append(argv, command...)
	return argv, nil
}

// RunSandboxLauncher is the hidden `pier __sandbox-exec` entrypoint: parse the
// writable paths, apply Landlock to this process, then exec the command. It
// returns the exit status for cmd.Execute to use.
func RunSandboxLauncher(args []string) int {
	// Landlock's restrict_self applies to the calling OS thread, and it must be
	// the same thread that execs: pin the goroutine so the Go scheduler cannot
	// migrate it between the two syscalls (observed as an unenforced run under
	// -race, when scheduling is busier).
	runtime.LockOSThread()
	var allowlist, command []string
	for i := 1; i < len(args); {
		switch args[i] {
		case "--allow":
			if i+1 >= len(args) {
				return sandboxLauncherFail("missing value for --allow")
			}
			allowlist = append(allowlist, args[i+1])
			i += 2
		case "--":
			command = args[i+1:]
			i = len(args)
		default:
			return sandboxLauncherFail("unexpected argument " + args[i])
		}
	}
	if len(allowlist) == 0 {
		return sandboxLauncherFail("no writable paths")
	}
	if len(command) == 0 {
		return sandboxLauncherFail("no command to run")
	}
	if err := applyLandlock(allowlist); err != nil {
		return sandboxLauncherFail(err.Error())
	}
	resolved, err := exec.LookPath(command[0])
	if err != nil {
		resolved = command[0]
	}
	argv := append([]string{resolved}, command[1:]...)
	if err := unix.Exec(resolved, argv, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "sandbox: exec %s: %v\n", resolved, err)
		return 127
	}
	return 0
}

func sandboxLauncherFail(message string) int {
	fmt.Fprintf(os.Stderr, "sandbox: %s — command refused\n", message)
	return 125
}

// applyLandlock builds and applies the ruleset: read+execute everywhere, full
// access under each writable path.
func applyLandlock(allowlist []string) error {
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 || int(abi) < 1 {
		if errno != 0 {
			return fmt.Errorf("Landlock unavailable: %v", errno)
		}
		return fmt.Errorf("Landlock unavailable (ABI %d)", int(abi))
	}
	handled := uint64(landlockReadBits | landlockWriteBits)
	// REFER (ABI 2) is denied by default once MAKE_* is handled; granting it
	// under already-writable trees keeps link/rename working with no new write
	// surface. TRUNCATE joined the handled set in the same ABI.
	if int(abi) >= 2 {
		handled |= unix.LANDLOCK_ACCESS_FS_TRUNCATE | unix.LANDLOCK_ACCESS_FS_REFER
	}
	rulesetFd, err := landlockCreateRuleset(handled)
	if err != nil {
		return err
	}
	defer unix.Close(rulesetFd)
	if err := landlockAddRule(rulesetFd, uint64(landlockReadBits), "/"); err != nil {
		return err
	}
	for _, path := range allowlist {
		if err := landlockAddRule(rulesetFd, handled, path); err != nil {
			return err
		}
	}
	// Unprivileged callers must set no_new_privs; it also blocks setuid
	// escalation inside the sandbox.
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("sandbox: prctl(NO_NEW_PRIVS): %v", err)
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(rulesetFd), 0, 0); errno != 0 {
		return fmt.Errorf("sandbox: landlock_restrict_self: %v", errno)
	}
	return nil
}

func landlockCreateRuleset(handled uint64) (int, error) {
	attr := unix.LandlockRulesetAttr{Access_fs: handled}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return -1, fmt.Errorf("sandbox: landlock_create_ruleset: %v", errno)
	}
	return int(fd), nil
}

func landlockAddRule(rulesetFd int, access uint64, path string) error {
	pathFd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("sandbox: cannot open %q: %v", path, err)
	}
	defer unix.Close(pathFd)
	attr := unix.LandlockPathBeneathAttr{Allowed_access: access, Parent_fd: int32(pathFd)}
	if _, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(rulesetFd), unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&attr)), 0, 0, 0); errno != 0 {
		return fmt.Errorf("sandbox: allow %q: %v", path, errno)
	}
	return nil
}
