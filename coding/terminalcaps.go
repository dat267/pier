package coding

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Port of packages/tui/src/terminal-image.ts detectCapabilitiesFromEnvironment, the
// half this port needs. The tui package injects terminal capabilities instead of
// detecting them itself (D69), so the detection lives here and the CLI installs the
// result at boot.
//
// Only hyperlinks are decided here. The image transports are out of scope (D57/D81)
// and the renderer consults no color-depth flag, so neither is reported.

// tmuxTermfeaturesTimeoutMS bounds the tmux probe; upstream allows it 250 ms.
const tmuxTermfeaturesTimeoutMS = 250

// HyperlinksSupported reports whether the terminal at the end of the current
// environment re-emits OSC 8 hyperlinks. The default is no: a terminal that swallows
// the escape renders the link as plain text and the URL disappears from the output,
// so only the terminals upstream recognizes are answered yes.
func HyperlinksSupported(env func(string) string, platform string, tmuxForwardsHyperlinks func() bool) bool {
	if env == nil {
		env = os.Getenv
	}
	termProgram := strings.ToLower(env("TERM_PROGRAM"))
	terminalEmulator := strings.ToLower(env("TERMINAL_EMULATOR"))
	term := strings.ToLower(env("TERM"))
	switch {
	case env("TMUX") != "" || strings.HasPrefix(term, "tmux"):
		// tmux strips OSC 8 unless its client advertises that it forwards it.
		return tmuxForwardsHyperlinks()
	case strings.HasPrefix(term, "screen"):
		// screen does not forward OSC 8 at all.
		return false
	case termProgram == "herdr":
		// Herdr forwards OSC 8, and it runs inside another terminal whose variables
		// (KITTY_WINDOW_ID, for example) can leak into its panes, so it is checked
		// before them (upstream #10573).
		return true
	case env("KITTY_WINDOW_ID") != "" || termProgram == "kitty":
		return true
	case termProgram == "ghostty" || strings.Contains(term, "ghostty") || env("GHOSTTY_RESOURCES_DIR") != "":
		return true
	case env("WEZTERM_PANE") != "" || termProgram == "wezterm":
		return true
	case termProgram == "warpterminal" || env("WARP_SESSION_ID") != "" || env("WARP_TERMINAL_SESSION_UUID") != "":
		return true
	case env("ITERM_SESSION_ID") != "" || termProgram == "iterm.app":
		return true
	case env("WT_SESSION") != "":
		return true
	case termProgram == "alacritty" || termProgram == "vscode" || termProgram == "zed":
		return true
	case terminalEmulator == "jetbrains-jediterm":
		return false
	case platform == "windows":
		// A modern Windows console is truecolor, but nothing about being one implies
		// OSC 8 support, so it still needs a positive detection above.
		return false
	default:
		return false
	}
}

// TmuxForwardsHyperlinks asks the attached tmux client whether it re-emits OSC 8
// hyperlinks, which is what its client_termfeatures lists. Every failure reports no
// forwarding, including tmux not being installed.
func TmuxForwardsHyperlinks() bool {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxTermfeaturesTimeoutMS*time.Millisecond)
	defer cancel()
	output, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "#{client_termfeatures}").Output()
	if err != nil {
		return false
	}
	for _, feature := range strings.Split(string(output), ",") {
		if strings.TrimSpace(feature) == "hyperlinks" {
			return true
		}
	}
	return false
}
