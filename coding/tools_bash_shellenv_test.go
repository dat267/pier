package coding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pi v1.5.2 (b4afdddf), utils/shell.ts getShellEnv prepends getBinDir()
// (<agentDir>/bin) to the PATH entry when it is absent, keeps the spelling of the
// key it finds case-insensitively, and leaves the value alone when the directory
// is already listed.
func TestShellEnvPrependsTheAgentBinDir(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	binDir := filepath.Join(agentDir, "bin")
	delimiter := string(os.PathListSeparator)

	cases := []struct {
		name    string
		pathKey string
		value   string
		want    string
	}{
		{"windows spelling", "Path", "C:\\one" + delimiter + "C:\\two", binDir + delimiter + "C:\\one" + delimiter + "C:\\two"},
		{"upper case", "PATH", "/one" + delimiter + "/two", binDir + delimiter + "/one" + delimiter + "/two"},
		{"already listed", "PATH", "/one" + delimiter + binDir + delimiter + "/two", "/one" + delimiter + binDir + delimiter + "/two"},
		{"empty value", "PATH", "", binDir},
		{"only delimiters", "PATH", delimiter + delimiter, binDir + delimiter + delimiter + delimiter},
	}
	type savedEnv struct {
		value   string
		present bool
	}
	// Both spellings are saved and restored: a case that only restored the key it
	// set would leave the process without the real PATH for every later test.
	save := func(key string) savedEnv {
		value, present := os.LookupEnv(key)
		return savedEnv{value: value, present: present}
	}
	originals := map[string]savedEnv{"PATH": save("PATH"), "Path": save("Path")}
	t.Cleanup(func() {
		for _, key := range []string{"PATH", "Path"} {
			original := originals[key]
			if original.present {
				_ = os.Setenv(key, original.value)
				continue
			}
			_ = os.Unsetenv(key)
		}
	})
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			for _, key := range []string{"PATH", "Path"} {
				_ = os.Unsetenv(key)
			}
			if err := os.Setenv(testCase.pathKey, testCase.value); err != nil {
				t.Fatal(err)
			}

			var got string
			found := 0
			for _, entry := range shellEnv(nil, true) {
				key, value, ok := strings.Cut(entry, "=")
				if !ok || !strings.EqualFold(key, "path") {
					continue
				}
				if key == testCase.pathKey {
					got = value
					found++
				}
			}
			if found != 1 {
				t.Fatalf("found %d %q entries, want one", found, testCase.pathKey)
			}
			if got != testCase.want {
				t.Fatalf("%s = %q, want %q", testCase.pathKey, got, testCase.want)
			}
		})
	}
}
