package coding

import (
	"strconv"
	"strings"
	"sync/atomic"
)

// Port of the CLI color decision. Upstream prints diagnostics with chalk,
// whose color support comes from supports-color: the stream must be a terminal
// unless FORCE_COLOR says otherwise, and a handful of terminal and CI
// environment variables decide the level (main.ts prints these lines with
// chalk.red/chalk.yellow, so a pipe, a dumb terminal or an unknown CI gets
// plain text).
//
// The pinned chalk's supports-color does not read NO_COLOR, so neither does
// this decision; a controlled pty run of the reference with NO_COLOR=1 still
// colors the line. SetCLIColorsEnabled is the seam tests and callers use.
var cliColorsState struct {
	enabled atomic.Bool
	set     atomic.Bool
}

// SetCLIColorsEnabled overrides the CLI color decision.
func SetCLIColorsEnabled(enabled bool) {
	cliColorsState.enabled.Store(enabled)
	cliColorsState.set.Store(true)
}

// CLIColorsEnabled reports whether CLI diagnostics may carry ANSI color.
func CLIColorsEnabled() bool {
	if !cliColorsState.set.Load() {
		return true
	}
	return cliColorsState.enabled.Load()
}

// knownCINames are the CI providers supports-color colors without a terminal.
var knownCINames = []string{"TRAVIS", "CIRCLECI", "APPVEYOR", "GITLAB_CI", "GITHUB_ACTIONS", "BUILDKITE"}

// ResolveCLIColors is supports-color's boolean answer for one process: true
// when diagnostics may be colored. isTTY is stdout's terminal state (chalk
// detects on stdout, not stderr, so a colored error still appears when only
// stderr is redirected); getenv looks up one variable; goos selects the
// platform branch.
func ResolveCLIColors(isTTY bool, getenv func(string) (string, bool), goos string) bool {
	// forceLevel: 0 = forced off, >0 = forced on, -1 = unset, -2 = set but not a
	// number (JavaScript's NaN, which leaves the fallbacks in charge).
	forceLevel := -1
	if value, ok := getenv("FORCE_COLOR"); ok {
		switch value {
		case "true":
			forceLevel = 1
		case "false":
			forceLevel = 0
		case "":
			forceLevel = 1
		default:
			// parseInt stops at the first non-digit, so "2abc" is 2.
			level := 0
			parsed := 0
			for parsed < len(value) && value[parsed] >= '0' && value[parsed] <= '9' {
				parsed++
			}
			if parsed == 0 {
				forceLevel = -2
			} else if number, err := strconv.Atoi(value[:parsed]); err == nil {
				level = min(number, 3)
				forceLevel = level
			} else {
				forceLevel = -2
			}
		}
	}
	if forceLevel == 0 {
		return false
	}
	if !isTTY && forceLevel == -1 {
		return false
	}
	minimum := 0
	if forceLevel > 0 {
		minimum = forceLevel
	}
	if term, _ := getenv("TERM"); term == "dumb" {
		return minimum > 0
	}
	if goos == "windows" {
		// supports-color assumes the ANSI-capable Windows 10 builds.
		return true
	}
	if _, inCI := getenv("CI"); inCI {
		for _, name := range knownCINames {
			if _, ok := getenv(name); ok {
				return true
			}
		}
		if name, ok := getenv("CI_NAME"); ok && name == "codeship" {
			return true
		}
		return minimum > 0
	}
	if version, ok := getenv("TEAMCITY_VERSION"); ok {
		// supports-color: 9.1+ or a two-digit major.
		major := version
		if dot := strings.IndexByte(major, '.'); dot >= 0 {
			major = major[:dot]
		}
		if number, err := strconv.Atoi(major); err == nil && number >= 10 {
			return true
		}
		if strings.HasPrefix(version, "9.") || strings.HasPrefix(version, "10.") || (len(major) == 2 && version != "9.0") {
			return true
		}
		return false
	}
	if program, ok := getenv("TERM_PROGRAM"); ok {
		switch program {
		case "iTerm.app", "Apple_Terminal":
			return true
		}
	}
	if term, _ := getenv("TERM"); term != "" {
		lowered := strings.ToLower(term)
		if strings.HasSuffix(lowered, "color") || strings.HasSuffix(lowered, "colour") ||
			strings.Contains(lowered, "screen") || strings.Contains(lowered, "xterm") ||
			strings.Contains(lowered, "vt100") || strings.Contains(lowered, "vt220") ||
			strings.Contains(lowered, "rxvt") || strings.Contains(lowered, "color") ||
			strings.Contains(lowered, "ansi") || strings.Contains(lowered, "cygwin") ||
			strings.Contains(lowered, "linux") {
			return true
		}
	}
	if colorterm, ok := getenv("COLORTERM"); ok && colorterm != "" {
		return true
	}
	return minimum > 0
}
