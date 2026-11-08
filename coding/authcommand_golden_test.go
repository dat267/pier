package coding

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func equalInt64Ptr(left, right *int64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

// Reference: pi v1.5.0 (f9a26365) packages/coding-agent/src/cli/auth-command.ts
// `parseAuthCommand`. The parse decides whether an invocation prints a
// credential (`print-api-key`/`print-bearer-token`) or only checks readiness,
// which flags each subcommand accepts, and how `--min-expiry` units parse, so a
// drift here changes either the credential surface or a user-visible rejection.
// The generator calls the pinned parser with the app name pi, which is what
// parseAuthCommandNamed receives here, so the error texts compare byte for byte.
// Regenerate: node --experimental-strip-types coding/testdata/auth-command/generate.mjs ./pi > coding/testdata/auth-command/golden.json
func TestParseAuthCommandMatchesReference(t *testing.T) {
	raw, err := os.ReadFile("testdata/auth-command/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Args   []string `json:"args"`
		Result *struct {
			Kind        string   `json:"kind"`
			Args        []string `json:"args"`
			JSON        bool     `json:"json"`
			Credentials bool     `json:"credentials"`
			NoRefresh   bool     `json:"noRefresh"`
			MinExpiryMS *int64   `json:"minExpiryMs"`
			Error       string   `json:"error"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) < 30 {
		t.Fatalf("golden holds %d cases", len(fixtures))
	}
	kinds := map[string]bool{}
	for _, fixture := range fixtures {
		command, err := parseAuthCommandNamed("pi", fixture.Args)
		if fixture.Result == nil {
			if command != nil || err != nil {
				t.Errorf("%q parsed as %+v, %v; the reference returns undefined", fixture.Args, command, err)
			}
			continue
		}
		if fixture.Result.Error != "" {
			if err == nil || err.Error() != fixture.Result.Error {
				t.Errorf("%q error = %v; want %q", fixture.Args, err, fixture.Result.Error)
			}
			if command != nil {
				t.Errorf("%q returned a command alongside the error", fixture.Args)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q error = %v; the reference parsed it", fixture.Args, err)
			continue
		}
		kinds[fixture.Result.Kind] = true
		// A nil and an empty Args both mean "no stray arguments".
		if !slices.Equal(command.Args, fixture.Result.Args) ||
			string(command.Kind) != fixture.Result.Kind ||
			command.JSON != fixture.Result.JSON ||
			command.Credentials != fixture.Result.Credentials ||
			command.NoRefresh != fixture.Result.NoRefresh ||
			!equalInt64Ptr(command.MinExpiryMS, fixture.Result.MinExpiryMS) {
			t.Errorf("%q parsed as %+v; want kind=%s args=%v json=%v credentials=%v noRefresh=%v minExpiry=%v",
				fixture.Args, *command, fixture.Result.Kind, fixture.Result.Args, fixture.Result.JSON,
				fixture.Result.Credentials, fixture.Result.NoRefresh, fixture.Result.MinExpiryMS)
		}
	}
	// Every subcommand and both arms have to stay covered.
	for _, kind := range []string{"check", "api_key", "bearer_token"} {
		if !kinds[kind] {
			t.Errorf("the golden covers no %s invocation", kind)
		}
	}
}
