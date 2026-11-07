package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) core/tools/{find,ls,grep}.ts pass
// `{ maxLines: Number.MAX_SAFE_INTEGER }` where only the byte limit should
// apply, and the details record that value (`details.truncation = truncation`).
// Pier used math.MaxInt, so a persisted truncation detail carried
// 9223372036854775807 instead of 9007199254740991.
func TestUncappedTruncationDetailsUseMaxSafeInteger(t *testing.T) {
	const maxSafeInteger = 9007199254740991
	dir := t.TempDir()
	// One long line: the byte limit fires while the line limit must not.
	long := "needle " + strings.Repeat("z", 60000)
	if err := os.WriteFile(filepath.Join(dir, "long.txt"), []byte(long+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for index := range 900 {
		name := fmt.Sprintf("f%03d-%s.txt", index, strings.Repeat("n", 60))
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	check := func(label string, details json.RawMessage) {
		t.Helper()
		if len(details) == 0 {
			t.Fatalf("%s: no details", label)
		}
		var parsed struct {
			Truncation *struct {
				MaxLines int64 `json:"maxLines"`
			} `json:"truncation"`
		}
		if err := json.Unmarshal(details, &parsed); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if parsed.Truncation == nil {
			t.Fatalf("%s: no truncation in %s", label, details)
		}
		if got := parsed.Truncation.MaxLines; got != maxSafeInteger {
			t.Errorf("%s: maxLines = %d, want %d", label, got, maxSafeInteger)
		}
	}

	// ls caps entries before bytes, so its fixture needs long names to cross
	// the 50KB byte limit within the 500-entry cap.
	lsDir := t.TempDir()
	for index := range 600 {
		name := fmt.Sprintf("e%03d-%s.txt", index, strings.Repeat("m", 120))
		if err := os.WriteFile(filepath.Join(lsDir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	findResult, err := CreateFindTool(dir).Execute("c", json.RawMessage(`{"pattern":"*"}`), context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	check("find", findResult.Details)

	lsResult, err := CreateLsTool(lsDir).Execute("c", json.RawMessage(`{"path":"."}`), context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	check("ls", lsResult.Details)

}
