package coding

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/dat267/pier/ai"
)

// Port of utils/shell.ts (shell resolution and binary-output sanitizing).

// ShellConfig describes how to invoke a shell.
type ShellConfig struct {
	Shell string
	Args  []string
	// CommandTransport is "argv" (default) or "stdin".
	CommandTransport string
}

// PowerShellArgs are the PowerShell invocation arguments.
var PowerShellArgs = []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command"}

// isLegacyWslBashPath detects the System32 bash.exe stub that needs stdin
// transport. The stub lives in the configured Windows directory, which need not
// be C:\Windows, so %SystemRoot% decides when it is set and the conventional
// location only applies when it is unknown (upstream v1.4.0 61060ddf8).
func isLegacyWslBashPath(path string) bool {
	normalized := strings.ToLower(normalizeWindowsPath(path))
	const launcher = `\bash.exe`
	if !strings.HasSuffix(normalized, launcher) {
		return false
	}
	// A drive letter, so a relative or UNC path cannot match.
	if !hasDrivePrefix(normalized) {
		return false
	}
	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		if !hasDrivePrefix(systemRoot) {
			return false
		}
		root := strings.TrimSuffix(strings.ToLower(normalizeWindowsPath(systemRoot)), `\`)
		for _, directory := range []string{"system32", "sysnative"} {
			if normalized == root+`\`+directory+launcher {
				return true
			}
		}
		return false
	}
	drive := normalized[:2]
	for _, directory := range []string{"system32", "sysnative"} {
		if normalized == drive+`\windows\`+directory+launcher {
			return true
		}
	}
	return false
}

// hasDrivePrefix reports whether path starts with a drive letter and a
// separator (upstream's `^[a-z]:[\\/]` guard).
func hasDrivePrefix(path string) bool {
	if len(path) < 3 || path[1] != ':' {
		return false
	}
	letter := path[0]
	if letter >= 'A' && letter <= 'Z' {
		letter |= 32
	}
	return letter >= 'a' && letter <= 'z' && (path[2] == '\\' || path[2] == '/')
}

// normalizeWindowsPath normalizes a path the way Node's win32.normalize does for
// the inputs here: separators, duplicate separators, "." and ".." segments, and
// a trailing separator. The drive prefix and the case are preserved.
func normalizeWindowsPath(path string) string {
	path = strings.ReplaceAll(path, "/", `\`)
	drive := ""
	if len(path) >= 2 && path[1] == ':' {
		drive = path[:2]
		path = path[2:]
	}
	var parts []string
	for _, part := range strings.Split(path, `\`) {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
			continue
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		if drive != "" {
			return drive + `\`
		}
		return `\`
	}
	return drive + `\` + strings.Join(parts, `\`)
}

// shellPathExists reports whether a shell candidate is runnable (upstream
// shellPathExists). Go's os.Stat resolves execution aliases, so the Node-specific
// existsSync workaround (#36790) has no counterpart here.
func shellPathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// firstExistingPath returns the first candidate that exists.
func firstExistingPath(candidates ...string) string {
	for _, candidate := range candidates {
		if candidate != "" && shellPathExists(candidate) {
			return candidate
		}
	}
	return ""
}

func bashShellConfig(shell string) ShellConfig {
	if isLegacyWslBashPath(shell) {
		return ShellConfig{Shell: shell, Args: []string{"-s"}, CommandTransport: "stdin"}
	}
	return ShellConfig{Shell: shell, Args: []string{"-c"}}
}

// findExecutableOnPath resolves an executable the way upstream does: `where`
// on Windows (verifying the file exists) and `which` elsewhere.
// firstOutputLine is upstream's `stdout.trim().split(/\r?\n/)[0]`.
func firstOutputLine(output []byte) string {
	trimmed := ai.JSTrim(DecodeUTF8WithReplacement(output))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, "\r\n", "\n")
	return strings.Split(trimmed, "\n")[0]
}

func findExecutableOnPath(executable string) string {
	if runtime.GOOS == "windows" {
		output, err := exec.Command("where", executable).Output()
		if err != nil {
			return ""
		}
		// Upstream checks only the first `where` match and rejects it when the
		// file does not exist.
		first := firstOutputLine(output)
		if first != "" {
			if _, err := os.Stat(first); err == nil {
				return first
			}
		}
		return ""
	}
	output, err := exec.Command("which", executable).Output()
	if err != nil {
		return ""
	}
	return firstOutputLine(output)
}

// GetShellConfig resolves the bash shell configuration (upstream
// getShellConfig).
func GetShellConfig(customShellPath string) (ShellConfig, error) {
	if customShellPath != "" {
		if shellPathExists(customShellPath) {
			return bashShellConfig(customShellPath), nil
		}
		return ShellConfig{}, fmt.Errorf("Custom shell path not found: %s", customShellPath)
	}

	if runtime.GOOS == "windows" {
		var paths []string
		if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
			paths = append(paths, filepath.Join(programFiles, "Git", "bin", "bash.exe"))
		}
		if programFilesX86 := os.Getenv("ProgramFiles(x86)"); programFilesX86 != "" {
			paths = append(paths, filepath.Join(programFilesX86, "Git", "bin", "bash.exe"))
		}
		for _, path := range paths {
			if shellPathExists(path) {
				return bashShellConfig(path), nil
			}
		}
		if bashOnPath := findExecutableOnPath("bash.exe"); bashOnPath != "" {
			return bashShellConfig(bashOnPath), nil
		}
		return ShellConfig{}, fmt.Errorf("%s", "No bash shell found. Options:\n"+
			"  1. Install Git for Windows: https://git-scm.com/download/win\n"+
			"  2. Add your bash to PATH (Cygwin, MSYS2, etc.)\n"+
			"  3. Set shellPath in settings.json\n\n"+
			"Searched Git Bash in:\n"+indentLines(paths))
	}

	if shellPathExists("/bin/bash") {
		return bashShellConfig("/bin/bash"), nil
	}
	if bashOnPath := findExecutableOnPath("bash"); bashOnPath != "" {
		return bashShellConfig(bashOnPath), nil
	}
	return ShellConfig{Shell: "sh", Args: []string{"-c"}}, nil
}

func indentLines(paths []string) string {
	var builder strings.Builder
	for index, path := range paths {
		if index > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString("  " + path)
	}
	return builder.String()
}

// GetPowerShellConfig resolves PowerShell, preferring PowerShell 7 (upstream
// getPowerShellConfig).
func GetPowerShellConfig() (ShellConfig, error) {
	return resolvePowerShellShell(runtime.GOOS, os.Getenv, findExecutableOnPath, shellPathExists)
}

// resolvePowerShellShell is the discovery order, with the platform and the two
// lookups injected so it is testable off Windows: pwsh.exe on PATH, then
// %ProgramFiles%\PowerShell\7\pwsh.exe, then powershell.exe on PATH, then
// %SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe. The
// installation directories may sit on another drive and need no PATH entry
// (upstream v1.4.0 61060ddf8).
func resolvePowerShellShell(goos string, getenv func(string) string, findOnPath func(string) string, exists func(string) bool) (ShellConfig, error) {
	if goos != "windows" {
		return ShellConfig{}, fmt.Errorf("The powershell tool is only available on Windows.")
	}
	shell := findOnPath("pwsh.exe")
	if shell == "" && getenv("ProgramFiles") != "" {
		shell = firstExisting(filepath.Join(getenv("ProgramFiles"), "PowerShell", "7", "pwsh.exe"), exists)
	}
	if shell == "" {
		shell = findOnPath("powershell.exe")
	}
	if shell == "" && getenv("SystemRoot") != "" {
		shell = firstExisting(filepath.Join(getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), exists)
	}
	if shell == "" {
		return ShellConfig{}, fmt.Errorf("No PowerShell executable found. Install PowerShell or add powershell.exe/pwsh.exe to PATH.")
	}
	return ShellConfig{Shell: shell, Args: append([]string{}, PowerShellArgs...)}, nil
}

// firstExisting returns path when it exists, and "" otherwise.
func firstExisting(path string, exists func(string) bool) string {
	if exists(path) {
		return path
	}
	return ""
}

// SanitizeBinaryOutput removes characters that crash display handling:
// control characters (except tab, newline, carriage return) and the Unicode
// Format range FFF9-FFFB (upstream sanitizeBinaryOutput).
//
// Clean text — the overwhelming majority — is returned as-is. The sanitizer
// runs over the whole accumulated tool output on every streaming result
// update, so rebuilding it rune by rune cost 670 us per update on a 155 KB
// build log (and quadratic work over a long command).
func SanitizeBinaryOutput(text string) string {
	if !hasBinaryOutput(text) {
		return text
	}
	var builder strings.Builder
	builder.Grow(len(text))
	start := 0
	for i := 0; i < len(text); {
		char, size := utf8.DecodeRuneInString(text[i:])
		if sanitizeDrops(char) || (char == utf8.RuneError && size <= 1) {
			if start < i {
				builder.WriteString(text[start:i])
			}
			if char == utf8.RuneError && size <= 1 {
				// A range over a string yields U+FFFD per invalid byte; keep
				// that substitution exactly.
				builder.WriteRune(utf8.RuneError)
			}
			i += size
			start = i
			continue
		}
		i += size
	}
	if start < len(text) {
		builder.WriteString(text[start:])
	}
	return builder.String()
}

// hasBinaryOutput reports whether SanitizeBinaryOutput would change text.
// ASCII — nearly all shell output — is tested a byte at a time; only non-ASCII
// bytes pay for a rune decode.
func hasBinaryOutput(text string) bool {
	for i := 0; i < len(text); i++ {
		char := text[i]
		if char < utf8.RuneSelf {
			if char < 0x20 && char != 0x09 && char != 0x0a && char != 0x0d {
				return true
			}
			continue
		}
		decoded, size := utf8.DecodeRuneInString(text[i:])
		if sanitizeDrops(decoded) || (decoded == utf8.RuneError && size <= 1) {
			return true
		}
		i += size - 1
	}
	return false
}

// sanitizeDrops reports whether a decoded rune is removed.
func sanitizeDrops(char rune) bool {
	return char <= 0x1f && char != 0x09 && char != 0x0a && char != 0x0d ||
		char >= 0xfff9 && char <= 0xfffb
}
