package coding

import (
	"encoding/json"
	"os"
	"testing"
)

// The pinned pi prints diagnostics with chalk, whose supports-color decision
// this ports (see coding/clicolor.go). The fixture is generated from the pinned
// supports-color itself, over environments, argv searches, stream states and
// both platform branches.
// Regenerate with node coding/testdata/clicolor/generate.mjs ./pi.
func TestResolveCLIColorsMatchesTheReference(t *testing.T) {
	data, err := os.ReadFile("testdata/clicolor/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Env     map[string]string `json:"env"`
		IsTTY   bool              `json:"isTTY"`
		Args    []string          `json:"args"`
		Goos    string            `json:"goos"`
		Level   any               `json:"level"`
		Colored bool              `json:"colored"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty color fixture")
	}
	for index, testCase := range cases {
		env := func(name string) (string, bool) {
			value, ok := testCase.Env[name]
			return value, ok
		}
		// The fixture records Node's platform names; the decision takes Go's.
		goos := testCase.Goos
		if goos == "win32" {
			goos = "windows"
		}
		if got := ResolveCLIColors(testCase.IsTTY, env, testCase.Args, goos); got != testCase.Colored {
			t.Fatalf("case %d %v tty=%v args=%v goos=%s: got %v, want %v (level %v)",
				index, testCase.Env, testCase.IsTTY, testCase.Args, testCase.Goos, got, testCase.Colored, testCase.Level)
		}
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
