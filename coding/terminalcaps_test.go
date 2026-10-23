package coding

import (
	"strings"
	"testing"
)

// Cases ported from packages/tui/test/terminal-image.test.ts, for the hyperlink half
// of detectCapabilitiesFromEnvironment that this port keeps (D69).
func TestHyperlinksSupported(t *testing.T) {
	cases := []struct {
		name     string
		env      map[string]string
		platform string
		tmux     bool
		want     bool
	}{
		{name: "nothing set", want: false},
		{name: "herdr", env: map[string]string{"TERM_PROGRAM": "herdr"}, want: true},
		{
			// Herdr may inherit another terminal's variables, so it is checked first.
			name: "herdr beats a leaked kitty window id",
			env:  map[string]string{"TERM_PROGRAM": "herdr", "KITTY_WINDOW_ID": "1"},
			want: true,
		},
		{name: "kitty by window id", env: map[string]string{"KITTY_WINDOW_ID": "1"}, want: true},
		{name: "kitty by term program", env: map[string]string{"TERM_PROGRAM": "kitty"}, want: true},
		{name: "rex", env: map[string]string{"TERM_PROGRAM": "rex"}, want: true},
		{name: "ghostty by term program", env: map[string]string{"TERM_PROGRAM": "ghostty"}, want: true},
		{name: "ghostty by term", env: map[string]string{"TERM": "xterm-ghostty"}, want: true},
		{name: "ghostty by resources dir", env: map[string]string{"GHOSTTY_RESOURCES_DIR": "/x"}, want: true},
		{name: "wezterm by pane", env: map[string]string{"WEZTERM_PANE": "0"}, want: true},
		{name: "wezterm by term program", env: map[string]string{"TERM_PROGRAM": "WezTerm"}, want: true},
		{name: "warp by session id", env: map[string]string{"WARP_SESSION_ID": "x"}, want: true},
		{name: "warp by terminal session uuid", env: map[string]string{"WARP_TERMINAL_SESSION_UUID": "x"}, want: true},
		{name: "warp by term program", env: map[string]string{"TERM_PROGRAM": "WarpTerminal"}, want: true},
		{name: "iterm by session id", env: map[string]string{"ITERM_SESSION_ID": "w0t0p0"}, want: true},
		{name: "iterm by term program", env: map[string]string{"TERM_PROGRAM": "iTerm.app"}, want: true},
		{name: "windows terminal", env: map[string]string{"WT_SESSION": "x"}, want: true},
		{name: "alacritty", env: map[string]string{"TERM_PROGRAM": "alacritty"}, want: true},
		{name: "vscode", env: map[string]string{"TERM_PROGRAM": "vscode"}, want: true},
		{name: "zed", env: map[string]string{"TERM_PROGRAM": "zed"}, want: true},
		{name: "screen does not forward", env: map[string]string{"TERM": "screen-256color"}, want: false},
		{
			name: "screen wins over what is inside it",
			env:  map[string]string{"TERM": "screen-256color", "KITTY_WINDOW_ID": "1"},
			want: false,
		},
		{name: "tmux asks the client", env: map[string]string{"TMUX": "/tmp/tmux-0,1,0"}, tmux: true, want: true},
		{name: "tmux client without the feature", env: map[string]string{"TMUX": "/tmp/tmux-0,1,0"}, tmux: false, want: false},
		{name: "tmux by term", env: map[string]string{"TERM": "tmux-256color"}, tmux: true, want: true},
		{name: "jediterm does not forward", env: map[string]string{"TERMINAL_EMULATOR": "JetBrains-JediTerm"}, want: false},
		{name: "unknown windows console", env: map[string]string{"TERM": "xterm-256color"}, platform: "windows", want: false},
		{name: "unknown terminal", env: map[string]string{"TERM": "xterm-256color"}, want: false},
		{name: "apple terminal", env: map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, want: false},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			platform := testCase.platform
			if platform == "" {
				platform = "linux"
			}
			probeCalls := 0
			probe := func() bool {
				probeCalls++
				return testCase.tmux
			}
			env := func(key string) string { return testCase.env[key] }
			if got := HyperlinksSupported(env, platform, probe); got != testCase.want {
				t.Fatalf("HyperlinksSupported = %v, want %v", got, testCase.want)
			}
			// The probe costs a subprocess, so only tmux may reach for it.
			wantCalls := 0
			if testCase.env["TMUX"] != "" || strings.HasPrefix(strings.ToLower(testCase.env["TERM"]), "tmux") {
				wantCalls = 1
			}
			if probeCalls != wantCalls {
				t.Fatalf("tmux probe calls = %d, want %d", probeCalls, wantCalls)
			}
		})
	}
}

// Cases ported from packages/tui/test/terminal-image.test.ts detectCapabilities,
// covering the image protocol and the PI_* overrides.
func TestDetectTerminalCapabilities(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		env        map[string]string
		platform   string
		tmux       bool
		wantImages string
		wantTrue   bool
		wantLinks  bool
	}{
		{name: "unknown terminal", env: map[string]string{"TERM": "xterm-256color"}},
		{name: "kitty", env: map[string]string{"KITTY_WINDOW_ID": "1"}, wantImages: "kitty", wantTrue: true, wantLinks: true},
		{name: "ghostty", env: map[string]string{"TERM_PROGRAM": "ghostty"}, wantImages: "kitty", wantTrue: true, wantLinks: true},
		{name: "wezterm", env: map[string]string{"TERM_PROGRAM": "wezterm"}, wantImages: "kitty", wantTrue: true, wantLinks: true},
		{name: "warp", env: map[string]string{"TERM_PROGRAM": "WarpTerminal"}, wantImages: "kitty", wantTrue: true, wantLinks: true},
		{name: "iterm2", env: map[string]string{"TERM_PROGRAM": "iterm.app"}, wantImages: "iterm2", wantTrue: true, wantLinks: true},
		{name: "herdr", env: map[string]string{"TERM_PROGRAM": "herdr", "KITTY_WINDOW_ID": "1"}, wantImages: "kitty", wantLinks: true},
		// Rex implements enough of the Kitty graphics protocol for pi's images
		// and forwards OSC 8; a leaked KITTY_WINDOW_ID must not decide it
		// (upstream 0ed1b0431 and 396ecd7b8, #11131).
		{name: "rex", env: map[string]string{"TERM_PROGRAM": "rex", "TERM": "xterm-256color"}, wantImages: "kitty", wantTrue: true, wantLinks: true},
		{name: "rex with a leaked kitty window id", env: map[string]string{"TERM_PROGRAM": "rex", "KITTY_WINDOW_ID": "1"}, wantImages: "kitty", wantTrue: true, wantLinks: true},
		{name: "windows terminal", env: map[string]string{"WT_SESSION": "x"}, wantTrue: true, wantLinks: true},
		{name: "tmux keeps no images", env: map[string]string{"TMUX": "/tmp/t,1,0", "TERM_PROGRAM": "ghostty"}, tmux: true, wantLinks: true},
		{
			name:       "PI_IMAGE_PROTOCOL and PI_TRUE_COLOR override",
			env:        map[string]string{"PI_IMAGE_PROTOCOL": "kitty", "PI_TRUE_COLOR": "1", "PI_HYPERLINKS": "1"},
			wantImages: "kitty", wantTrue: true, wantLinks: true,
		},
		{
			name:       "PI_IMAGE_PROTOCOL none clears a detected protocol",
			env:        map[string]string{"TERM_PROGRAM": "iterm.app", "PI_IMAGE_PROTOCOL": "none"},
			wantImages: "", wantTrue: true, wantLinks: true,
		},
		{
			name:       "PI_IMAGE_PROTOCOL auto keeps detection",
			env:        map[string]string{"TERM_PROGRAM": "ghostty", "PI_IMAGE_PROTOCOL": "auto"},
			wantImages: "kitty", wantTrue: true, wantLinks: true,
		},
		{
			name:       "PI_HYPERLINKS 0 forces hyperlinks off without probing",
			env:        map[string]string{"PI_HYPERLINKS": "0", "PI_IMAGE_PROTOCOL": "kitty", "PI_TRUE_COLOR": "0"},
			wantImages: "kitty", wantTrue: false, wantLinks: false,
		},
		{
			name:       "rex honors PI_HYPERLINKS 0",
			env:        map[string]string{"TERM_PROGRAM": "rex", "PI_HYPERLINKS": "0"},
			wantImages: "kitty", wantTrue: true, wantLinks: false,
		},
		{
			name:       "PI_TRUE_COLOR 0 overrides a detected truecolor",
			env:        map[string]string{"KITTY_WINDOW_ID": "1", "PI_TRUE_COLOR": "0"},
			wantImages: "kitty", wantTrue: false, wantLinks: true,
		},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			platform := testCase.platform
			if platform == "" {
				platform = "linux"
			}
			env := func(key string) string { return testCase.env[key] }
			got := DetectTerminalCapabilities(env, platform, func() bool { return testCase.tmux })
			if got.Images != testCase.wantImages || got.TrueColor != testCase.wantTrue || got.Hyperlinks != testCase.wantLinks {
				t.Fatalf("caps = %+v, want images=%q trueColor=%v hyperlinks=%v", got, testCase.wantImages, testCase.wantTrue, testCase.wantLinks)
			}
			if testCase.name == "PI_HYPERLINKS 0 forces hyperlinks off without probing" && testCase.tmux {
				t.Fatal("probe should not be trusted")
			}
		})
	}
}
