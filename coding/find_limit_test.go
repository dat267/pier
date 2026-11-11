package coding

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// installFindStub puts a fake fd on PATH that prints the given lines whatever
// arguments it gets, so the tool's own limit handling is what is exercised.
func installFindStub(t *testing.T, lines ...string) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\n"
	for _, line := range lines {
		script += "printf '" + line + "\\n'\n"
	}
	if err := os.WriteFile(filepath.Join(bin, "fd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func findToolText(t *testing.T, params string) (string, json.RawMessage) {
	t.Helper()
	result, err := CreateFindTool(t.TempDir()).Execute("call", json.RawMessage(params), context.Background(), nil)
	if err != nil {
		t.Fatalf("find %s: %v", params, err)
	}
	text, ok := result.Content[0].(ai.TextContent)
	if !ok {
		t.Fatalf("content = %+v", result.Content)
	}
	return text.Text, result.Details
}

// pi v1.5.2 (b4afdddf) core/tools/find.ts asks fd for one result past the limit,
// so the limit notice only appears when more matches exist, and keeps only the
// limit's worth of results. A limit below 1 means 1, because fd treats
// --max-results 0 as unlimited, and the limit is floored like the schema's Number.
func TestFindToolLimitBoundaryAndFloor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX executable stub")
	}
	installFindStub(t, "a.txt", "b.txt", "c.txt")

	// Exactly as many matches as the limit: no notice.
	text, details := findToolText(t, `{"pattern":"*.txt","limit":3}`)
	if text != "a.txt\nb.txt\nc.txt" {
		t.Fatalf("limit 3 = %q, want the three matches and no notice", text)
	}
	if len(details) != 0 {
		t.Fatalf("limit 3 details = %s", details)
	}

	// One match over the limit: notice, and the extra result is not shown.
	text, details = findToolText(t, `{"pattern":"*.txt","limit":2}`)
	if text != "a.txt\nb.txt\n\n[2 results limit reached. Use limit=4 for more, or refine pattern]" {
		t.Fatalf("limit 2 = %q", text)
	}
	if len(details) == 0 || !strings.Contains(string(details), `"resultLimitReached":2`) {
		t.Fatalf("limit 2 details = %s", details)
	}

	// A fractional limit floors.
	text, _ = findToolText(t, `{"pattern":"*.txt","limit":2.9}`)
	if !strings.HasPrefix(text, "a.txt\nb.txt\n\n[2 results limit reached.") {
		t.Fatalf("limit 2.9 = %q", text)
	}

	// Zero and negative limits mean one, never "unlimited".
	for _, limit := range []string{"0", "-5"} {
		text, details = findToolText(t, `{"pattern":"*.txt","limit":`+limit+`}`)
		if !strings.HasPrefix(text, "a.txt\n\n[1 results limit reached. Use limit=2 for more, or refine pattern]") {
			t.Fatalf("limit %s = %q", limit, text)
		}
		if len(details) == 0 || !strings.Contains(string(details), `"resultLimitReached":1`) {
			t.Fatalf("limit %s details = %s", limit, details)
		}
	}
}
