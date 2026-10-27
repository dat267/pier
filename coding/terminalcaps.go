package coding

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/dat267/pier/tui"
)

// Port of packages/tui/src/terminal-image.ts detectCapabilities and
// detectCapabilitiesFromEnvironment. The tui package injects terminal
// capabilities instead of detecting them itself (D69), so the detection lives
// here and the CLI installs the result at boot.

// tmuxTermfeaturesTimeoutMS bounds the tmux probe; upstream allows it 250 ms.
const tmuxTermfeaturesTimeoutMS = 250

// detectedTerminalCapabilities is the environment-derived capability triplet.
type detectedTerminalCapabilities struct {
	images     string
	trueColor  bool
	hyperlinks bool
}

// detectTerminalCapabilities mirrors upstream's environment switch. The image
// protocols are only enabled for terminals upstream positively recognizes;
// tmux, screen and an unknown terminal report none.
func detectTerminalCapabilities(env func(string) string, platform string, tmuxForwardsHyperlinks func() bool) detectedTerminalCapabilities {
	if env == nil {
		env = os.Getenv
	}
	termProgram := strings.ToLower(env("TERM_PROGRAM"))
	terminalEmulator := strings.ToLower(env("TERMINAL_EMULATOR"))
	term := strings.ToLower(env("TERM"))
	colorTerm := strings.ToLower(env("COLORTERM"))
	hasTrueColorHint := colorTerm == "truecolor" || colorTerm == "24bit" || strings.HasSuffix(term, "-direct")
	isWindowsConsole := platform == "windows"

	switch {
	case env("TMUX") != "" || strings.HasPrefix(term, "tmux"):
		// tmux strips OSC 8 unless its client forwards it, and image protocols
		// are unreliable through it.
		return detectedTerminalCapabilities{images: "", trueColor: hasTrueColorHint, hyperlinks: tmuxForwardsHyperlinks()}
	case strings.HasPrefix(term, "screen"):
		return detectedTerminalCapabilities{images: "", trueColor: hasTrueColorHint, hyperlinks: false}
	case termProgram == "herdr":
		// Herdr forwards OSC 8 and supports kitty graphics; it is checked
		// before KITTY_WINDOW_ID, which can leak into its panes (#10573).
		return detectedTerminalCapabilities{images: tui.ImageProtocolKitty, trueColor: hasTrueColorHint, hyperlinks: true}
	case termProgram == "rex":
		// Rex forwards OSC 8 and implements enough of the Kitty graphics protocol
		// for pi's images (no Unicode placeholders). Checked before the outer
		// terminal checks, so a leaked KITTY_WINDOW_ID does not decide it
		// (upstream 0ed1b0431 and 396ecd7b8, #11131).
		return detectedTerminalCapabilities{images: tui.ImageProtocolKitty, trueColor: true, hyperlinks: true}
	case env("KITTY_WINDOW_ID") != "" || termProgram == "kitty":
		return detectedTerminalCapabilities{images: tui.ImageProtocolKitty, trueColor: true, hyperlinks: true}
	case termProgram == "ghostty" || strings.Contains(term, "ghostty") || env("GHOSTTY_RESOURCES_DIR") != "":
		return detectedTerminalCapabilities{images: tui.ImageProtocolKitty, trueColor: true, hyperlinks: true}
	case env("WEZTERM_PANE") != "" || termProgram == "wezterm":
		return detectedTerminalCapabilities{images: tui.ImageProtocolKitty, trueColor: true, hyperlinks: true}
	case termProgram == "warpterminal" || env("WARP_SESSION_ID") != "" || env("WARP_TERMINAL_SESSION_UUID") != "":
		return detectedTerminalCapabilities{images: tui.ImageProtocolKitty, trueColor: true, hyperlinks: true}
	case env("ITERM_SESSION_ID") != "" || termProgram == "iterm.app":
		return detectedTerminalCapabilities{images: tui.ImageProtocolITerm2, trueColor: true, hyperlinks: true}
	case env("WT_SESSION") != "":
		return detectedTerminalCapabilities{images: "", trueColor: true, hyperlinks: true}
	case termProgram == "alacritty" || termProgram == "vscode" || termProgram == "zed":
		return detectedTerminalCapabilities{images: "", trueColor: true, hyperlinks: true}
	case terminalEmulator == "jetbrains-jediterm":
		return detectedTerminalCapabilities{images: "", trueColor: true, hyperlinks: false}
	case isWindowsConsole:
		return detectedTerminalCapabilities{images: "", trueColor: true, hyperlinks: false}
	default:
		return detectedTerminalCapabilities{images: "", trueColor: hasTrueColorHint, hyperlinks: false}
	}
}

// HyperlinksSupported reports whether the terminal at the end of the current
// environment re-emits OSC 8 hyperlinks. The default is no: a terminal that
// swallows the escape renders the link as plain text and the URL disappears
// from the output, so only the terminals upstream recognizes are answered yes.
func HyperlinksSupported(env func(string) string, platform string, tmuxForwardsHyperlinks func() bool) bool {
	return detectTerminalCapabilities(env, platform, tmuxForwardsHyperlinks).hyperlinks
}

func parseBooleanCapabilityOverride(value string) (bool, bool) {
	switch value {
	case "1":
		return true, true
	case "0":
		return false, true
	}
	return false, false
}

// DetectTerminalCapabilities resolves the terminal capabilities the renderer
// uses (upstream detectCapabilities). PI_HYPERLINKS, PI_IMAGE_PROTOCOL and
// PI_TRUE_COLOR override the environment detection; "auto" and any other value
// keep the detected result.
func DetectTerminalCapabilities(env func(string) string, platform string, tmuxForwardsHyperlinks func() bool) tui.TerminalCapabilities {
	if env == nil {
		env = os.Getenv
	}
	probe := tmuxForwardsHyperlinks
	hyperlinksOverride, hasHyperlinksOverride := false, false
	if value, ok := parseBooleanCapabilityOverride(env("PI_HYPERLINKS")); ok {
		hyperlinksOverride, hasHyperlinksOverride = value, true
		probe = func() bool { return value }
	}
	detected := detectTerminalCapabilities(env, platform, probe)
	hyperlinks := detected.hyperlinks
	if hasHyperlinksOverride {
		hyperlinks = hyperlinksOverride
	}

	images := detected.images
	switch strings.ToLower(env("PI_IMAGE_PROTOCOL")) {
	case "kitty", "iterm2":
		images = strings.ToLower(env("PI_IMAGE_PROTOCOL"))
	case "none", "0":
		images = ""
	}
	trueColor := detected.trueColor
	if value, ok := parseBooleanCapabilityOverride(env("PI_TRUE_COLOR")); ok {
		trueColor = value
	}
	return tui.TerminalCapabilities{Images: images, TrueColor: trueColor, Hyperlinks: hyperlinks}
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
	for feature := range strings.SplitSeq(string(output), ",") {
		if strings.TrimSpace(feature) == "hyperlinks" {
			return true
		}
	}
	return false
}
