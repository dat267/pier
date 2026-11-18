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

// ResolveCLIColors is supports-color's boolean answer for one process: true
// when diagnostics may be colored. isTTY is stdout's terminal state (chalk
// detects on stdout, not stderr, so a colored error still appears when only
// stderr is redirected); getenv looks up one variable; args are the process
// arguments after argv[0], whose color flags supports-color also honors; goos
// selects the platform branch.
func ResolveCLIColors(isTTY bool, getenv func(string) (string, bool), args []string, goos string) bool {
	forceLevel, forced := resolveForceColorLevel(getenv, args)
	if forced && forceLevel == 0 {
		return false
	}
	// Azure DevOps reports color capability before the terminal check.
	if _, ok := getenv("TF_BUILD"); ok {
		if _, ok := getenv("AGENT_NAME"); ok {
			return true
		}
	}
	if !isTTY && !forced {
		return false
	}
	minimum := 0
	if forced && forceLevel > 0 {
		minimum = forceLevel
	}
	if term, _ := getenv("TERM"); term == "dumb" {
		return minimum > 0
	}
	if goos == "windows" {
		// supports-color assumes the ANSI-capable Windows 10 builds; every level
		// it returns there is truthy.
		return true
	}
	if _, inCI := getenv("CI"); inCI {
		for _, name := range []string{"GITHUB_ACTIONS", "GITEA_ACTIONS", "CIRCLECI"} {
			if _, ok := getenv(name); ok {
				return true
			}
		}
		for _, name := range []string{"TRAVIS", "APPVEYOR", "GITLAB_CI", "BUILDKITE", "DRONE"} {
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
		return teamCityColors(version)
	}
	if colorterm, ok := getenv("COLORTERM"); ok && colorterm == "truecolor" {
		return true
	}
	term, _ := getenv("TERM")
	switch term {
	case "xterm-kitty", "xterm-ghostty", "wezterm":
		return true
	}
	if program, ok := getenv("TERM_PROGRAM"); ok {
		switch program {
		case "iTerm.app", "Apple_Terminal":
			return true
		}
	}
	lowered := strings.ToLower(term)
	if strings.HasSuffix(lowered, "-256") || strings.HasSuffix(lowered, "-256color") {
		return true
	}
	if hasAnyPrefix(lowered, "screen", "xterm", "vt100", "vt220", "rxvt") ||
		strings.Contains(lowered, "color") || strings.Contains(lowered, "ansi") ||
		strings.Contains(lowered, "cygwin") || strings.Contains(lowered, "linux") {
		return true
	}
	if _, ok := getenv("COLORTERM"); ok {
		return true
	}
	return minimum > 0
}

func hasAnyPrefix(value string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

// resolveForceColorLevel ports supports-color's envForceColor and the argv
// flags createSupportsColor sniffs. The level is 0 for off, 1..3 for forced on;
// forced is false when neither source constrains the decision. FORCE_COLOR wins
// over the flags, and an invalid FORCE_COLOR (anything that does not parse into
// 0..3, including the text parseInt cannot start on) leaves the flags in charge.
func resolveForceColorLevel(getenv func(string) (string, bool), args []string) (int, bool) {
	flagLevel, hasFlag := 0, false
	flag := func(name string) bool { return hasArgument(args, name) }
	switch {
	case flag("no-color") || flag("no-colors") || flag("color=false") || flag("color=never"):
		flagLevel, hasFlag = 0, true
	case flag("color") || flag("colors") || flag("color=true") || flag("color=always"):
		flagLevel, hasFlag = 1, true
	case flag("color=16m") || flag("color=full") || flag("color=truecolor"):
		flagLevel, hasFlag = 3, true
	case flag("color=256"):
		flagLevel, hasFlag = 2, true
	}

	value, ok := getenv("FORCE_COLOR")
	if !ok {
		return flagLevel, hasFlag
	}
	switch value {
	case "true":
		return 1, true
	case "false":
		return 0, true
	case "":
		return 1, true
	}
	// parseInt stops at the first non-digit, so "2abc" is 2.
	parsed := 0
	for parsed < len(value) && value[parsed] >= '0' && value[parsed] <= '9' {
		parsed++
	}
	if parsed == 0 {
		return flagLevel, hasFlag
	}
	number, err := strconv.Atoi(value[:parsed])
	if err != nil {
		return flagLevel, hasFlag
	}
	level := min(number, 3)
	if level < 0 {
		return flagLevel, hasFlag
	}
	return level, true
}

// hasArgument ports supports-color's hasFlag: a long flag matches as "--name",
// a one-letter flag as "-name", and anything after a bare "--" is not a flag.
func hasArgument(args []string, flag string) bool {
	prefix := "--"
	if len(flag) == 1 {
		prefix = "-"
	}
	wanted := prefix + flag
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == wanted {
			return true
		}
	}
	return false
}

// teamCityColors ports supports-color's /^(9\.(0*[1-9]\d*)\.|\d{2,}\.)/ test:
// TeamCity 9.1 and later, or a two-digit-or-longer major version.
func teamCityColors(version string) bool {
	if minor, rest, ok := strings.Cut(version, "."); ok {
		if minor == "9" {
			if dot := strings.IndexByte(rest, '.'); dot > 0 {
				digits := strings.TrimLeft(rest[:dot], "0")
				if digits == "" || digits[0] < '1' || digits[0] > '9' {
					return false
				}
				for _, char := range digits {
					if char < '0' || char > '9' {
						return false
					}
				}
				return true
			}
			return false
		}
		if len(minor) >= 2 {
			for _, char := range minor {
				if char < '0' || char > '9' {
					return false
				}
			}
			return true
		}
	}
	return false
}
