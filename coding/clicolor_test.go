package coding

import "testing"

// The pinned pi prints diagnostics with chalk, whose supports-color decision
// this ports (see coding/clicolor.go). Each case is a value observed from the
// reference 1.5.0 binary: a piped stderr is plain, a pty colors, TERM=dumb and
// an unnamed CI are plain, a named CI and FORCE_COLOR color, and NO_COLOR is
// ignored because the pinned supports-color does not read it.
func TestResolveCLIColorsMatchesTheReference(t *testing.T) {
	envOf := func(values map[string]string) func(string) (string, bool) {
		return func(name string) (string, bool) {
			value, ok := values[name]
			return value, ok
		}
	}
	cases := []struct {
		name  string
		isTTY bool
		env   map[string]string
		goos  string
		want  bool
	}{
		{name: "pipe", env: map[string]string{"TERM": "xterm-256color"}},
		{name: "pty", isTTY: true, env: map[string]string{"TERM": "xterm-256color"}, want: true},
		{name: "pty dumb", isTTY: true, env: map[string]string{"TERM": "dumb"}},
		{name: "pty no term", isTTY: true},
		{name: "pty unknown term", isTTY: true, env: map[string]string{"TERM": "weird"}},
		{name: "pty no color ignored", isTTY: true, env: map[string]string{"TERM": "xterm", "NO_COLOR": "1"}, want: true},
		{name: "pipe forced", env: map[string]string{"FORCE_COLOR": "1"}, want: true},
		{name: "pipe forced empty", env: map[string]string{"FORCE_COLOR": ""}, want: true},
		{name: "pipe forced true", env: map[string]string{"FORCE_COLOR": "true"}, want: true},
		{name: "pty forced off", isTTY: true, env: map[string]string{"TERM": "xterm", "FORCE_COLOR": "0"}},
		{name: "pty forced false", isTTY: true, env: map[string]string{"TERM": "xterm", "FORCE_COLOR": "false"}},
		{name: "forced off beats dumb", isTTY: true, env: map[string]string{"TERM": "dumb", "FORCE_COLOR": "2"}, want: true},
		{name: "pty named ci", isTTY: true, env: map[string]string{"TERM": "xterm", "CI": "1", "GITHUB_ACTIONS": "1"}, want: true},
		{name: "pty unnamed ci", isTTY: true, env: map[string]string{"TERM": "xterm", "CI": "1"}},
		{name: "pipe named ci", env: map[string]string{"CI": "1", "GITHUB_ACTIONS": "1"}},
		{name: "pty colorterm", isTTY: true, env: map[string]string{"COLORTERM": "truecolor"}, want: true},
		{name: "pty apple terminal", isTTY: true, env: map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, want: true},
		{name: "pty screen", isTTY: true, env: map[string]string{"TERM": "screen-256color"}, want: true},
		// supports-color returns 0 for a non-tty stream before the platform
		// branch, so Windows colors only a terminal.
		{name: "windows pipe", goos: "windows"},
		{name: "windows pty", isTTY: true, goos: "windows", want: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			goos := testCase.goos
			if goos == "" {
				goos = "linux"
			}
			if got := ResolveCLIColors(testCase.isTTY, envOf(testCase.env), goos); got != testCase.want {
				t.Fatalf("ResolveCLIColors = %t, want %t", got, testCase.want)
			}
		})
	}
}

func TestFormatCLIDiagnosticDropsColorWhenDisabled(t *testing.T) {
	previous := CLIColorsEnabled()
	SetCLIColorsEnabled(false)
	t.Cleanup(func() { SetCLIColorsEnabled(previous) })
	if got := FormatCLIDiagnostic(CLIDiagnostic{Type: "error", Message: "Unknown option: --x"}); got != "Error: Unknown option: --x" {
		t.Fatalf("plain = %q", got)
	}
	if got := FormatCLIWarning("Warning: x"); got != "Warning: x" {
		t.Fatalf("plain warning = %q", got)
	}
	SetCLIColorsEnabled(true)
	if got := FormatCLIWarning("Warning: x"); got != "\x1b[33mWarning: x\x1b[39m" {
		t.Fatalf("colored warning = %q", got)
	}
}
